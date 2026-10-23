package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/gorilla/websocket"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// providerDiscord is the provider whose sources the manager runs sessions for.
const providerDiscord = "discord"

const (
	defaultGatewayURL        = "wss://gateway.discord.gg"
	gatewayReconcileInterval = 30 * time.Second
	// Discord allows one Identify per 5 seconds for each max_concurrency slot.
	gatewayIdentifySpacing = 5 * time.Second
	gatewayBackoffBase     = time.Second
	gatewayBackoffMax      = 5 * time.Minute
	gatewayOpenTimeout     = time.Minute
	gatewayCloseTimeout    = 5 * time.Second
	gatewayIngestTimeout   = 30 * time.Second
	gatewayStatusTimeout   = 10 * time.Second
	gatewayMessageQueue    = 256

	// GUILDS, GUILD_MESSAGES and DIRECT_MESSAGES: 4609, none privileged (ADR Decision 4).
	gatewayIntents = gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentDirectMessages
)

// Reasons recorded on a source when Discord rejects its session.
const (
	reasonBadToken   = "discord rejected the bot token"
	reasonBadIntents = "discord rejected the requested intents"
)

// GatewayConfig tunes the manager. A zero field takes the default.
type GatewayConfig struct {
	// URL is the Gateway URL. Defaults to wss://gateway.discord.gg.
	URL string
	// RESTURL replaces Discord's REST API URL; tests point it at a fake.
	RESTURL           string
	ReconcileInterval time.Duration
	IdentifySpacing   time.Duration
	BackoffBase       time.Duration
	BackoffMax        time.Duration

	// gatewayOpts are appended to the Gateway options; tests turn off the
	// resume URL and compression through them.
	gatewayOpts []gateway.ConfigOpt
}

func (c GatewayConfig) withDefaults() GatewayConfig {
	c.URL = cmpOr(c.URL, defaultGatewayURL)
	c.ReconcileInterval = cmpOr(c.ReconcileInterval, gatewayReconcileInterval)
	c.IdentifySpacing = cmpOr(c.IdentifySpacing, gatewayIdentifySpacing)
	c.BackoffBase = cmpOr(c.BackoffBase, gatewayBackoffBase)
	c.BackoffMax = cmpOr(c.BackoffMax, gatewayBackoffMax)
	return c
}

func cmpOr[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// gatewayStore is the storage the manager touches.
type gatewayStore interface {
	ListGatewaySources(ctx context.Context, provider string) ([]WebhookSource, error)
	SetGatewayStatus(ctx context.Context, sourceID string, rotatedAt *time.Time, status, reason string) error
}

// gatewayIngester hands a Gateway message to the pipeline.
type gatewayIngester interface {
	IngestGateway(ctx context.Context, sourceID string, body []byte) (IngestResult, error)
}

// adapterLookup finds the adapter the manager asks for event types.
type adapterLookup interface {
	Get(provider string) (SourceAdapter, bool)
}

// tokenDecrypter opens a source's stored bot token.
type tokenDecrypter interface {
	decrypt(ciphertext string) (string, error)
}

// GatewayManager holds one Gateway session per enabled Discord source that has
// a secret, and reconciles them against webhook_sources. Every replica runs
// one; replicas share nothing and the delivery unique index keeps one delivery
// per message (ADR Decisions 2a and 3).
type GatewayManager struct {
	store    gatewayStore
	ingest   gatewayIngester
	adapters adapterLookup
	cipher   tokenDecrypter
	logger   logging.Logger
	cfg      GatewayConfig
	limiter  *identifyLimiter
	session  func(ctx context.Context, s *gatewaySession) (reachedReady bool, err error) // connect; a seam for tests

	wake     chan struct{}
	cancel   context.CancelFunc
	loopDone chan struct{}
	wg       sync.WaitGroup // the sessions' ingest workers
}

// NewGatewayManager creates a manager. It does not run until Start.
func NewGatewayManager(
	store gatewayStore,
	ingest gatewayIngester,
	adapters adapterLookup,
	cipher tokenDecrypter,
	cfg GatewayConfig,
	logger logging.Logger,
) *GatewayManager {
	cfg = cfg.withDefaults()
	m := &GatewayManager{
		store:    store,
		ingest:   ingest,
		adapters: adapters,
		cipher:   cipher,
		logger:   logger,
		cfg:      cfg,
		limiter:  newIdentifyLimiter(cfg.IdentifySpacing),
		wake:     make(chan struct{}, 1),
		loopDone: make(chan struct{}),
	}
	m.session = m.connect
	return m
}

// Start reconciles at once and then on the interval. It does not block.
func (m *GatewayManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)
	go m.loop(ctx)
	m.logger.Info(ctx, "discord gateway manager started")
}

// Stop ends every session and waits for them, or for ctx.
func (m *GatewayManager) Stop(ctx context.Context) {
	if m.cancel == nil {
		return
	}
	m.cancel()
	select {
	case <-m.loopDone:
	case <-ctx.Done():
	}
	m.logger.Info(ctx, "discord gateway manager stopped")
}

// Reconcile asks for a pass at once. The replica that served a source change
// calls it; the others find the change at their next pass.
func (m *GatewayManager) Reconcile() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// gatewaySession is one running source session.
type gatewaySession struct {
	sourceID  string
	rotatedAt *time.Time
	token     string
	adapter   SourceAdapter
	cancel    context.CancelFunc
	done      chan struct{}
	msgs      chan []byte // wrapped messages for the ingest worker, in arrival order
}

func (m *GatewayManager) loop(ctx context.Context) {
	defer close(m.loopDone)
	sessions := make(map[string]*gatewaySession)
	defer func() {
		m.stopSessions(sessions, func(string) bool { return true })
		m.wg.Wait()
	}()

	ticker := time.NewTicker(m.cfg.ReconcileInterval)
	defer ticker.Stop()
	for {
		m.reconcile(ctx, sessions)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}

// reconcile starts, stops and restarts sessions to match the sources table.
func (m *GatewayManager) reconcile(ctx context.Context, sessions map[string]*gatewaySession) {
	adapter, ok := m.adapters.Get(providerDiscord)
	if !ok {
		return
	}
	srcs, err := m.store.ListGatewaySources(ctx, providerDiscord)
	if err != nil {
		m.logger.Error(ctx, "list gateway sources failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	want := make(map[string]*WebhookSource, len(srcs))
	for i := range srcs {
		want[srcs[i].ID] = &srcs[i]
	}
	m.stopSessions(sessions, func(id string) bool {
		src, ok := want[id]
		return !ok || !sameInstant(src.SecretRotatedAt, sessions[id].rotatedAt)
	})
	for id, src := range want {
		if _, running := sessions[id]; running || rejectedSince(src) {
			continue
		}
		token, err := m.cipher.decrypt(src.SecretCiphertext)
		if err != nil {
			m.logger.Error(ctx, "decrypt bot token failed",
				logging.Field{Key: "source_id", Value: id}, logging.Field{Key: "error", Value: err.Error()})
			continue
		}
		sctx, cancel := context.WithCancel(ctx)
		s := &gatewaySession{
			sourceID: id, rotatedAt: src.SecretRotatedAt, token: token, adapter: adapter,
			cancel: cancel, done: make(chan struct{}), msgs: make(chan []byte, gatewayMessageQueue),
		}
		sessions[id] = s
		m.wg.Add(1)
		go m.ingestLoop(sctx, s)
		go func() {
			defer cancel()
			m.run(sctx, s)
		}()
	}
}

// stopSessions ends the sessions pick selects and waits for them to close.
func (m *GatewayManager) stopSessions(sessions map[string]*gatewaySession, pick func(id string) bool) {
	var stopped []*gatewaySession
	for id, s := range sessions {
		if pick(id) {
			s.cancel()
			stopped = append(stopped, s)
			delete(sessions, id)
		}
	}
	for _, s := range stopped {
		<-s.done
	}
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// rejectedSince reports a recorded rejection newer than the secret: the session
// stays stopped until the secret changes (ADR Decision 3).
func rejectedSince(src *WebhookSource) bool {
	return src.GatewayStatus == connRejected && src.GatewayStatusAt != nil &&
		src.SecretRotatedAt != nil && src.GatewayStatusAt.After(*src.SecretRotatedAt)
}

// run keeps a session open until it is stopped or Discord rejects it, waiting
// between sessions with a backoff that a session reaching Ready resets.
func (m *GatewayManager) run(ctx context.Context, s *gatewaySession) {
	defer close(s.done)
	failures := 0
	for {
		if failures > 0 {
			t := time.NewTimer(m.backoff(failures))
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
		reachedReady, err := m.session(ctx, s)
		if ctx.Err() != nil {
			return
		}
		if reachedReady {
			failures = 0
		}
		if reason, rejected := rejectionReason(err); rejected {
			m.logger.Error(ctx, "discord rejected the gateway session",
				logging.Field{Key: "source_id", Value: s.sourceID}, logging.Field{Key: "reason", Value: reason})
			m.recordRejection(ctx, s, reason)
			return
		}
		failures++
		m.logger.Warn(ctx, "gateway session ended; starting a new one after a backoff",
			logging.Field{Key: "source_id", Value: s.sourceID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

// recordRejection writes the rejection until it lands: a lost write would let
// the next process retry Identify with a token Discord already refused.
func (m *GatewayManager) recordRejection(ctx context.Context, s *gatewaySession, reason string) {
	for failures := 1; ctx.Err() == nil; failures++ {
		if m.setStatus(ctx, s, connRejected, reason) == nil {
			return
		}
		t := time.NewTimer(m.backoff(failures))
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
}

// backoff is exponential from BackoffBase to BackoffMax, jittered over its upper half.
func (m *GatewayManager) backoff(failures int) time.Duration {
	d := m.cfg.BackoffMax
	if failures <= 32 {
		d = min(m.cfg.BackoffBase<<(failures-1), m.cfg.BackoffMax)
	}
	if d <= 0 {
		d = m.cfg.BackoffMax
	}
	return d/2 + rand.N(d/2+1) // #nosec G404 — jitter is not security-sensitive
}

// rejectionReason maps a close Discord marked not reconnectable to the reason
// recorded on the source.
func rejectionReason(err error) (string, bool) {
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		return "", false
	}
	code := gateway.CloseEventCodeByCode(ce.Code)
	if code.Reconnect {
		return "", false
	}
	switch ce.Code {
	case gateway.CloseEventCodeAuthenticationFailed.Code:
		return reasonBadToken, true
	case gateway.CloseEventCodeDisallowedIntent.Code:
		return reasonBadIntents, true
	}
	return code.Description, true
}

// koraContext is the part of the stored body that is not the Discord payload.
type koraContext struct {
	BotUserID string      `json:"bot_user_id"`
	Thread    *koraThread `json:"thread,omitempty"`
}

type koraThread struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	ParentType int    `json:"parent_type"`
	OwnerID    string `json:"owner_id"`
}

// connect opens one session and blocks until it ends. reachedReady reports
// whether it got to Ready; err is why it ended, nil when ctx stopped it.
func (m *GatewayManager) connect(ctx context.Context, s *gatewaySession) (reachedReady bool, err error) {
	var (
		client    *bot.Client
		botUserID atomic.Value
		closed    = make(chan error, 1)
	)
	client, err = disgo.New(s.token,
		bot.WithRestClientConfigOpts(m.restOpts()...),
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagChannels)),
		bot.WithGatewayConfigOpts(append([]gateway.ConfigOpt{
			gateway.WithURL(m.cfg.URL),
			gateway.WithIntents(gatewayIntents),
			gateway.WithEnableRawEvents(true),
			gateway.WithIdentifyRateLimiter(m.limiter),
			gateway.WithCloseHandler(func(_ gateway.Gateway, err error, _ bool) {
				select {
				case closed <- err:
				default:
				}
			}),
		}, m.cfg.gatewayOpts...)...),
		bot.WithEventListenerFunc(func(e *events.Ready) {
			botUserID.Store(e.User.ID.String())
			// Synchronous: a rejection written after this must not be overwritten by a late connected.
			_ = m.setStatus(ctx, s, connConnected, "") // logged; the next Ready writes it again
		}),
		bot.WithEventListenerFunc(func(e *events.Raw) {
			if e.EventType != gateway.EventTypeMessageCreate {
				return
			}
			id, _ := botUserID.Load().(string)
			m.onMessage(ctx, s, client, id, e.Payload)
		}),
	)
	if err != nil {
		return false, fmt.Errorf("build discord client: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayCloseTimeout)
		defer cancel()
		client.Close(closeCtx)
	}()

	openCtx, cancel := context.WithTimeout(ctx, gatewayOpenTimeout)
	defer cancel()
	if err := client.OpenGateway(openCtx); err != nil {
		return false, fmt.Errorf("open gateway: %w", err)
	}
	select {
	case <-ctx.Done():
		return true, nil
	case err := <-closed:
		return true, err
	}
}

func (m *GatewayManager) restOpts() []rest.ClientConfigOpt {
	if m.cfg.RESTURL == "" {
		return nil
	}
	return []rest.ClientConfigOpt{rest.WithURL(m.cfg.RESTURL)}
}

// onMessage wraps a MESSAGE_CREATE payload with what the cache knows (ADR
// Decisions 6 and 7) and ingests it when the adapter finds an event in it.
func (m *GatewayManager) onMessage(ctx context.Context, s *gatewaySession, client *bot.Client, botUserID string, payload io.Reader) {
	raw, err := io.ReadAll(payload)
	if err != nil {
		m.logger.Error(ctx, "read gateway message failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	var head struct {
		ChannelID snowflake.ID `json:"channel_id"`
	}
	if err = json.Unmarshal(raw, &head); err != nil {
		m.logger.Error(ctx, "parse gateway message failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	body, err := json.Marshal(struct {
		D    json.RawMessage `json:"d"`
		Kora koraContext     `json:"kora"`
	}{raw, koraContext{BotUserID: botUserID, Thread: threadOf(client.Caches, head.ChannelID)}})
	if err != nil {
		m.logger.Error(ctx, "wrap gateway message failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	if s.adapter.EventType(nil, body) == "" {
		return
	}
	select {
	case s.msgs <- body:
	default:
		m.logger.Error(ctx, "gateway message dropped: ingest queue full", logging.Field{Key: "source_id", Value: s.sourceID})
	}
}

// ingestLoop ingests a session's messages in order, off the read loop that must keep answering heartbeats.
func (m *GatewayManager) ingestLoop(ctx context.Context, s *gatewaySession) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case body := <-s.msgs:
			ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayIngestTimeout)
			if _, err := m.ingest.IngestGateway(ictx, s.sourceID, body); err != nil {
				m.logger.Error(ictx, "ingest gateway message failed",
					logging.Field{Key: "source_id", Value: s.sourceID}, logging.Field{Key: "error", Value: err.Error()})
			}
			cancel()
		}
	}
}

// threadOf reads the channel cache: a cached thread, with its parent's type, or nil.
func threadOf(caches cache.Caches, channelID snowflake.ID) *koraThread {
	ch, ok := caches.Channel(channelID)
	if !ok {
		return nil
	}
	th, ok := ch.(discord.GuildThread)
	if !ok || th.ParentID() == nil {
		return nil
	}
	t := &koraThread{ID: th.ID().String(), ParentID: th.ParentID().String(), OwnerID: th.OwnerID.String()}
	if parent, ok := caches.Channel(*th.ParentID()); ok {
		t.ParentType = int(parent.Type())
	}
	return t
}

// setStatus records the session's state, unless the source's secret has changed.
func (m *GatewayManager) setStatus(ctx context.Context, s *gatewaySession, status, reason string) error {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayStatusTimeout)
	defer cancel()
	err := m.store.SetGatewayStatus(sctx, s.sourceID, s.rotatedAt, status, reason)
	if err != nil {
		m.logger.Error(ctx, "record gateway status failed",
			logging.Field{Key: "source_id", Value: s.sourceID}, logging.Field{Key: "error", Value: err.Error()})
	}
	return err
}

// identifyLimiter admits one Identify at a time and spaces them, across every
// session of the manager. Resumes do not call it.
type identifyLimiter struct {
	slot    chan struct{} // holds a token while an Identify is in flight
	mu      sync.Mutex
	next    time.Time
	spacing time.Duration
}

func newIdentifyLimiter(spacing time.Duration) *identifyLimiter {
	return &identifyLimiter{slot: make(chan struct{}, 1), spacing: spacing}
}

func (l *identifyLimiter) Wait(ctx context.Context, _ int) error {
	select {
	case l.slot <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait for identify slot: %w", ctx.Err())
	}
	l.mu.Lock()
	d := time.Until(l.next)
	l.mu.Unlock()
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		<-l.slot
		return fmt.Errorf("wait for identify slot: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

func (l *identifyLimiter) Unlock(int) {
	l.mu.Lock()
	l.next = time.Now().Add(l.spacing)
	l.mu.Unlock()
	<-l.slot
}

func (l *identifyLimiter) Close(context.Context) {}

// connectionOf is the read-only view of a Gateway source's state.
func connectionOf(src *WebhookSource) *SourceConnection {
	switch {
	case src.SecretCiphertext == "":
		return &SourceConnection{State: connWaiting}
	case src.GatewayStatus == "":
		return &SourceConnection{State: connConnecting}
	}
	return &SourceConnection{State: src.GatewayStatus, Reason: src.GatewayReason}
}
