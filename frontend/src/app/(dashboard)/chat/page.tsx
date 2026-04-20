'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Bot,
  Code2,
  Copy,
  RotateCcw,
  Send,
  Shield,
  Sparkles,
  Terminal,
  ThumbsDown,
  ThumbsUp,
  Zap,
} from 'lucide-react';

import { Badge, Button, Grid, Stack, Text, Heading, Textarea, cn } from '@orbit/terra';

// ─── Types ────────────────────────────────────────────────────────────────────

type MessageRole = 'user' | 'assistant';

interface Message {
  id: string;
  role: MessageRole;
  content: string;
  isStreaming?: boolean;
  timestamp: Date;
}

// ─── Constants ────────────────────────────────────────────────────────────────

const MODELS = ['Orbit-3 Opus', 'Orbit-3 Sonnet', 'Orbit-3 Haiku'] as const;

const SUGGESTIONS = [
  {
    icon: Shield,
    title: 'Policy engine deep dive',
    desc: 'How does OPA integration work in Orbit?',
    prompt:
      'Explain how the Orbit policy engine works with OPA integration, including how policies are evaluated before agent actions execute.',
  },
  {
    icon: Bot,
    title: 'Agent authentication flow',
    desc: 'Walk me through identity verification',
    prompt:
      'Walk me through how agent authentication and identity verification works in Orbit, from SPIFFE SVID to session JWT.',
  },
  {
    icon: Terminal,
    title: 'Generate a Go service template',
    desc: 'Bootstrap a new feature package',
    prompt:
      'Generate a Go service template for a new Orbit feature package following the DDD-lite repository pattern.',
  },
  {
    icon: Code2,
    title: 'Credential broker pattern',
    desc: 'How are credentials scoped and issued?',
    prompt:
      'Explain the credential broker pattern and how scoped just-in-time credentials are provisioned in Orbit.',
  },
] as const;

// ─── Mock AI responses ────────────────────────────────────────────────────────

const RESPONSES = {
  policy: `Orbit's policy engine is built on Open Policy Agent (OPA), which evaluates structured Rego policies before any agent action executes — never after.

Here's the end-to-end evaluation flow:

1. An agent requests an action (e.g. "write to S3 bucket")
2. The policy engine intercepts the request before execution
3. OPA evaluates the request against the loaded Rego bundle
4. A decision is returned: allow, deny, or conditional
5. Only allowed actions proceed to the credential broker

The key principle: policies are a pre-execution gate, not a post-hoc audit log.

\`\`\`go
// internal/policy/service.go
func (s *Service) Enforce(ctx context.Context, req Request) (Decision, error) {
    input := map[string]any{
        "agent":    req.AgentIdentity,
        "action":   req.Action,
        "resource": req.Resource,
        "time":     time.Now().UTC(),
    }

    result, err := s.opa.Evaluate(ctx, "orbit/authz/allow", input)
    if err != nil {
        return Decision{}, fmt.Errorf("policy evaluation: %w", err)
    }

    return Decision{Allowed: result.Allowed, Reason: result.Reason}, nil
}
\`\`\`

Policy bundles are versioned Rego files that live alongside your infrastructure code, loaded into the OPA engine at startup and hot-reloaded on change.`,

  auth: `Agent authentication in Orbit follows a zero-trust, identity-first model. Every agent must prove its identity before any action is permitted — there are no anonymous operations.

**The Authentication Flow**

1. **Identity Registration** — Agents present a SPIFFE SVID (X.509 certificate) issued by a trusted Workload API
2. **Token Exchange** — The SVID is exchanged for an Orbit-issued session JWT at the \`/identity/svid/exchange\` endpoint
3. **JWT Validation** — All requests carry the JWT; the platform validates the signature and checks the JTI blocklist in Redis
4. **Context Propagation** — Identity flows through the full execution chain via \`context.Context\`

\`\`\`go
// Agent bootstraps identity on startup
svid, err := workload.FetchX509SVID(ctx)
if err != nil {
    return fmt.Errorf("SVID fetch: %w", err)
}

resp, err := identityClient.ExchangeSVID(ctx, &identity.ExchangeRequest{
    Svid: svid.Marshal(),
})
if err != nil {
    return fmt.Errorf("token exchange: %w", err)
}

// resp.Token is used for all subsequent Orbit API calls
agent.SetToken(resp.Token)
\`\`\`

This ensures every Orbit operation is tied to a verified, auditable identity — a core principle of the nervous-system architecture.`,

  template: `Here's a complete Go feature package template following Orbit's DDD-lite architecture. Each feature owns its full stack: repository interface, PostgreSQL implementation, service layer, and HTTP handlers.

\`\`\`go
// internal/myfeature/models.go
package myfeature

import "time"

type MyEntity struct {
    ID        string    \`db:"id"\`
    Name      string    \`db:"name"\`
    CreatedAt time.Time \`db:"created_at"\`
    UpdatedAt time.Time \`db:"updated_at"\`
}
\`\`\`

\`\`\`go
// internal/myfeature/repository.go
package myfeature

import "context"

// Repository defines the storage contract for this feature.
// The PostgreSQL implementation lives in postgres.go.
type Repository interface {
    Create(ctx context.Context, e *MyEntity) error
    GetByID(ctx context.Context, id string) (*MyEntity, error)
    List(ctx context.Context) ([]*MyEntity, error)
    Delete(ctx context.Context, id string) error
}
\`\`\`

\`\`\`go
// internal/myfeature/service.go
package myfeature

import (
    "context"
    "fmt"

    "github.com/orbitapp/orbit/internal/platform/logging"
)

type Service struct {
    repo Repository
    log  *logging.Logger
}

func NewService(repo Repository, log *logging.Logger) *Service {
    return &Service{repo: repo, log: log}
}

func (s *Service) Create(ctx context.Context, name string) (*MyEntity, error) {
    e := &MyEntity{ID: generateID(), Name: name}
    if err := s.repo.Create(ctx, e); err != nil {
        return nil, fmt.Errorf("create entity: %w", err)
    }
    s.log.Info(ctx, "entity created", "id", e.ID)
    return e, nil
}
\`\`\`

Key conventions: no ORMs (raw SQL via pgx/v5), inject \`*logging.Logger\` never use globals, features never import each other — share only through platform contracts.`,

  credential: `The credential broker is Orbit's "motor cortex" — it translates policy decisions into scoped, time-limited access grants that agents can act on without ever seeing raw credentials.

**Core Principles**

- Agents never receive master credentials — only derived, scoped tokens
- All access is just-in-time (JIT): minted per-operation, not per-session
- Scope is derived from the policy decision, not from agent claims
- Every issuance is recorded on the event spine for full auditability

**The Issuance Flow**

\`\`\`
Agent Request
      │
      ▼
Policy Engine ──► DENY ──► 403 Forbidden
      │
      ▼  ALLOW + scope set
Credential Broker
      │
      ├── Extract scope from policy decision
      ├── Mint credential (Vault, AWS STS, GCP WIF…)
      ├── Enforce TTL (minutes, never hours)
      └── Emit issuance event to spine
      │
      ▼
Scoped token returned to agent
\`\`\`

\`\`\`go
// internal/broker/service.go
type BrokerRequest struct {
    AgentID   string
    PolicyRef string
    Scope     []string      // derived from policy, never from agent
    TTL       time.Duration // maximum 15 minutes
}

func (s *Service) Issue(ctx context.Context, req BrokerRequest) (*Credential, error) {
    // Scope is validated against the policy decision — agents
    // cannot escalate beyond what policy explicitly granted.
    if err := s.validateScope(ctx, req.PolicyRef, req.Scope); err != nil {
        return nil, ErrScopeEscalation
    }
    return s.provider.Mint(ctx, req)
}
\`\`\`

Even a fully compromised agent can only access what was explicitly scoped for that specific operation window.`,

  fallback: `That's a great question about Orbit's control plane architecture.

Orbit is the nervous system for autonomous agents — providing coordination, identity, and governance infrastructure rather than being a framework, model router, or LLM wrapper.

The four pillars of the architecture:

1. **Identity-first** — Every action is tied to a verified, auditable identity (OIDC/SPIFFE)
2. **Policy-governed** — OPA policies evaluate before execution, never after the fact
3. **Observable** — All agent behaviour flows through the structured event spine
4. **Least-privilege** — Credentials are scoped, time-limited, and issued just-in-time

Is there a specific aspect of Orbit you'd like me to dig deeper into? I can explain the policy engine, credential broker, agent identity lifecycle, event spine architecture, or help you generate Go service templates.`,
};

// ─── Helpers ──────────────────────────────────────────────────────────────────

function generateId(): string {
  return Math.random().toString(36).slice(2, 11);
}

function formatTime(date: Date): string {
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function getAIResponse(userInput: string): string {
  const lower = userInput.toLowerCase();
  if (lower.includes('policy') || lower.includes('opa') || lower.includes('rego')) {
    return RESPONSES.policy;
  }
  if (
    lower.includes('auth') ||
    lower.includes('identity') ||
    lower.includes('svid') ||
    lower.includes('jwt')
  ) {
    return RESPONSES.auth;
  }
  if (
    lower.includes('credential') ||
    lower.includes('broker') ||
    lower.includes('vault') ||
    lower.includes('sts')
  ) {
    return RESPONSES.credential;
  }
  if (
    lower.includes('go') ||
    lower.includes('service') ||
    lower.includes('template') ||
    lower.includes('package') ||
    lower.includes('scaffold')
  ) {
    return RESPONSES.template;
  }
  return RESPONSES.fallback;
}

// ─── TypingIndicator ──────────────────────────────────────────────────────────

function TypingIndicator() {
  return (
    <Stack direction="horizontal" align="center" gap="tight" asChild>
      {/* eslint-disable-next-line local/no-raw-div */}
      <div className="px-1 py-0.5">
        {[0, 1, 2].map((i) => (
          // eslint-disable-next-line local/no-raw-div
          <div
            key={i}
            className="h-1.5 w-1.5 rounded-full bg-muted animate-bounce"
            style={{ animationDelay: `${i * 150}ms`, animationDuration: '900ms' }}
          />
        ))}
      </div>
    </Stack>
  );
}

// ─── MessageContent ───────────────────────────────────────────────────────────

function MessageContent({ content }: { content: string }) {
  // Split on fenced code blocks, preserving them as separate segments.
  const segments = content.split(/(```(?:[a-z]*\n)?[\s\S]*?```)/g);

  return (
    <Stack gap="snug" asChild>
      <article className="text-sm leading-relaxed text-foreground">
        {segments.map((seg, i) => {
          const codeMatch = seg.match(/^```([a-z]*)\n?([\s\S]*?)```$/);
          if (codeMatch) {
            const lang = codeMatch[1] || 'code';
            const code = codeMatch[2].trim();
            return (
              <Stack key={i} gap="none" asChild>
                <figure className="rounded-lg border border-border-subtle overflow-hidden m-0">
                  <Stack direction="horizontal" align="center" justify="between" asChild>
                    <figcaption className="px-4 py-2 bg-surface border-b border-border-subtle">
                      <Text size="micro" color="subtle" mono>
                        {lang}
                      </Text>
                      {/* eslint-disable-next-line local/no-raw-div */}
                      <div className="-mr-1">
                        <Button variant="ghost" size="icon-sm" aria-label="Copy code">
                          <Copy className="h-3 w-3" />
                        </Button>
                      </div>
                    </figcaption>
                  </Stack>
                  <pre className="bg-background px-4 py-3 overflow-x-auto">
                    <code className="font-mono text-xs text-foreground leading-relaxed">
                      {code}
                    </code>
                  </pre>
                </figure>
              </Stack>
            );
          }
          // Plain text segment — preserve newlines.
          return seg ? (
            // eslint-disable-next-line local/no-raw-div
            <div key={i} className="whitespace-pre-wrap">
              {seg}
            </div>
          ) : null;
        })}
      </article>
    </Stack>
  );
}

// ─── ChatPage ─────────────────────────────────────────────────────────────────

export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [isThinking, setIsThinking] = useState(false);
  const [modelIdx, setModelIdx] = useState(0);

  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const intervalRef = useRef<number | null>(null);

  const currentModel = MODELS[modelIdx];
  const isStreaming = messages.some((m) => m.isStreaming);
  const canSubmit = input.trim().length > 0 && !isThinking && !isStreaming;

  // Auto-scroll to bottom when messages change.
  useEffect(() => {
    const el = scrollContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages]);

  // Clean up streaming interval on unmount.
  useEffect(() => {
    return () => {
      if (intervalRef.current !== null) clearInterval(intervalRef.current);
    };
  }, []);

  // ── Streaming simulation ──

  const streamResponse = useCallback((messageId: string, fullText: string) => {
    const words = fullText.split(' ');
    let idx = 0;
    const BATCH = 3; // words per tick

    const interval = window.setInterval(() => {
      idx = Math.min(idx + BATCH, words.length);
      const partial = words.slice(0, idx).join(' ');

      setMessages((prev) =>
        prev.map((m) =>
          m.id === messageId ? { ...m, content: partial, isStreaming: idx < words.length } : m,
        ),
      );

      if (idx >= words.length) {
        clearInterval(interval);
        intervalRef.current = null;
      }
    }, 35);

    intervalRef.current = interval;
  }, []);

  // ── Submit ──

  const handleSubmit = useCallback(() => {
    if (!canSubmit) return;

    const userMsg: Message = {
      id: generateId(),
      role: 'user',
      content: input.trim(),
      timestamp: new Date(),
    };

    setMessages((prev) => [...prev, userMsg]);
    setInput('');
    setIsThinking(true);

    // Reset textarea height after clearing value.
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }

    const responseText = getAIResponse(userMsg.content);
    const thinkingMs = 500 + Math.random() * 600;

    setTimeout(() => {
      const assistantMsg: Message = {
        id: generateId(),
        role: 'assistant',
        content: '',
        isStreaming: true,
        timestamp: new Date(),
      };
      setIsThinking(false);
      setMessages((prev) => [...prev, assistantMsg]);
      streamResponse(assistantMsg.id, responseText);
    }, thinkingMs);
  }, [canSubmit, input, streamResponse]);

  // ── Keyboard handler ──

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSubmit();
      }
    },
    [handleSubmit],
  );

  // ── Suggestion chips ──

  const handleSuggestion = useCallback((prompt: string) => {
    setInput(prompt);
    // Focus textarea so user can edit or just hit Enter.
    textareaRef.current?.focus();
  }, []);

  // ── Clear conversation ──

  const handleClear = useCallback(() => {
    if (intervalRef.current !== null) clearInterval(intervalRef.current);
    setMessages([]);
    setIsThinking(false);
    setInput('');
  }, []);

  // ── Cycle model ──

  const cycleModel = useCallback(() => {
    setModelIdx((i) => (i + 1) % MODELS.length);
  }, []);

  // ─────────────────────────────────────────────────────────────────────────────

  /*
   * h-full overflow-hidden: fills PageLayout's <main> (flex-1 overflow-y-auto)
   * exactly. The message area owns its own scroll context via flex-1
   * overflow-y-auto so the main element never scrolls itself.
   */
  return (
    <Stack direction="vertical" asChild>
      <section className="h-full overflow-hidden">
        {/* ── Message area ─────────────────────────────────────────────────────── */}
        <main ref={scrollContainerRef} className="flex-1 overflow-y-auto">
          {messages.length === 0 ? (
            /* Welcome screen */
            <Stack direction="vertical" align="center" justify="center" asChild>
              <section className="h-full px-6 py-12 text-center">
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="mb-6 flex h-16 w-16 items-center justify-center rounded-2xl bg-accent/15">
                  <Sparkles className="h-8 w-8 text-accent" />
                </div>
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="mb-2">
                  <Heading variant="section">How can Orbit help?</Heading>
                </div>
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="mb-10 max-w-sm">
                  <Text size="supporting" color="muted">
                    The AI-powered control plane assistant. Ask about agent architecture, generate
                    code, or explore Orbit&apos;s design.
                  </Text>
                </div>

                {/* Suggestion chips */}
                <Grid cols={{ base: 1, sm: 2 }} gap="snug" asChild>
                  <nav className="w-full max-w-xl">
                    {SUGGESTIONS.map((s) => {
                      const Icon = s.icon;
                      return (
                        <button
                          key={s.title}
                          onClick={() => handleSuggestion(s.prompt)}
                          className={cn(
                            'group flex flex-col items-start gap-1.5 rounded-xl p-4 text-left',
                            'border border-border bg-surface',
                            'hover:border-accent/50 hover:bg-accent/5',
                            'transition-colors duration-150',
                          )}
                        >
                          <Icon className="h-4 w-4 text-accent mb-0.5" />
                          <Text size="supporting" weight="emphasis">
                            {s.title}
                          </Text>
                          <Text size="caption" color="subtle">
                            {s.desc}
                          </Text>
                        </button>
                      );
                    })}
                  </nav>
                </Grid>
              </section>
            </Stack>
          ) : (
            /* Message thread */
            <Stack gap="spacious" asChild>
              <ol className="max-w-3xl mx-auto w-full px-4 py-6 list-none m-0 p-6">
                {messages.map((msg) => (
                  <Stack key={msg.id} direction="horizontal" gap="snug" asChild>
                    <li className={cn(msg.role === 'user' ? 'flex-row-reverse' : 'flex-row')}>
                      {/* Bubble */}
                      <Stack direction="vertical" gap="snug" asChild>
                        {/* eslint-disable-next-line local/no-raw-div */}
                        <div
                          className={cn(
                            'group relative max-w-[78%]',
                            msg.role === 'user' ? 'items-end' : 'items-start',
                          )}
                        >
                          {/* eslint-disable-next-line local/no-raw-div */}
                          <div
                            className={cn(
                              'rounded-2xl px-4 py-3',
                              msg.role === 'assistant'
                                ? 'bg-surface border border-border-subtle rounded-tl-sm'
                                : 'bg-accent/10 border border-accent/20 rounded-tr-sm',
                            )}
                          >
                            {msg.content ? (
                              <MessageContent content={msg.content} />
                            ) : (
                              /* Streaming start — show dots before first words arrive */
                              <TypingIndicator />
                            )}
                            {msg.isStreaming && msg.content && (
                              /* Blinking cursor during streaming */
                              // eslint-disable-next-line local/no-raw-div
                              <div className="inline-block ml-0.5 h-3.5 w-0.5 bg-accent align-middle animate-pulse" />
                            )}
                          </div>

                          {/* Timestamp + actions */}
                          <Stack direction="horizontal" align="center" gap="snug" asChild>
                            {/* eslint-disable-next-line local/no-raw-div */}
                            <div
                              className={cn(
                                'px-1',
                                'opacity-0 group-hover:opacity-100 transition-opacity',
                                msg.role === 'user' ? 'flex-row-reverse' : 'flex-row',
                              )}
                            >
                              <Text size="micro" color="subtle">
                                {formatTime(msg.timestamp)}
                              </Text>
                              {msg.role === 'assistant' && !msg.isStreaming && (
                                <>
                                  {/*
                                   * Message action buttons — raw <button>: icon-only
                                   * micro-actions in the hover overlay.
                                   */}
                                  <button
                                    className="text-subtle hover:text-foreground transition-colors"
                                    aria-label="Copy message"
                                  >
                                    <Copy className="h-3 w-3" />
                                  </button>
                                  <button
                                    className="text-subtle hover:text-status-healthy transition-colors"
                                    aria-label="Good response"
                                  >
                                    <ThumbsUp className="h-3 w-3" />
                                  </button>
                                  <button
                                    className="text-subtle hover:text-status-error transition-colors"
                                    aria-label="Bad response"
                                  >
                                    <ThumbsDown className="h-3 w-3" />
                                  </button>
                                </>
                              )}
                            </div>
                          </Stack>
                        </div>
                      </Stack>
                    </li>
                  </Stack>
                ))}

                {/* Thinking indicator (between user message and streaming start) */}
                {isThinking && (
                  <Stack direction="horizontal" gap="snug" asChild>
                    <li className="list-none">
                      {/* eslint-disable-next-line local/no-raw-div */}
                      <div className="rounded-2xl rounded-tl-sm px-4 py-3 bg-surface border border-border-subtle">
                        <TypingIndicator />
                      </div>
                    </li>
                  </Stack>
                )}
              </ol>
            </Stack>
          )}
        </main>

        {/* ── Input bar ────────────────────────────────────────────────────────── */}
        <Stack asChild>
          <footer className="shrink-0 bg-background px-4 pt-3 pb-4">
            <Stack asChild>
              <form className="max-w-3xl mx-auto w-full" onSubmit={(e) => e.preventDefault()}>
                {/* eslint-disable-next-line local/no-raw-div */}
                <div
                  className={cn(
                    'rounded-2xl border bg-surface transition-colors duration-150',
                    'focus-within:border-accent border-border',
                  )}
                >
                  {/*
                   * variant="bare": the outer span above owns the border, background,
                   * focus ring, and border-radius. The Textarea provides only the
                   * interaction surface — no redundant chrome of its own.
                   */}
                  <Textarea
                    ref={textareaRef}
                    variant="bare"
                    autoResize
                    rows={1}
                    maxRows={6}
                    placeholder="Message Orbit…"
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={handleKeyDown}
                    disabled={isThinking}
                  />

                  {/* Bottom toolbar */}
                  <Stack direction="horizontal" align="center" justify="between" asChild>
                    <footer className="px-3 pb-2.5 pt-1">
                      <Stack direction="horizontal" align="center" gap="snug">
                        {/*
                         * Model selector: clicking the badge cycles through MODELS.
                         * Raw <button> wrapper: we need onClick + hover on the badge
                         * without adding a Terra Button's fixed size around it.
                         */}
                        <button
                          onClick={cycleModel}
                          className="flex items-center gap-1 rounded-md hover:opacity-75 transition-opacity"
                          title="Click to change model"
                        >
                          <Badge variant="dim" size="sm">
                            <Zap className="h-2.5 w-2.5 mr-1" />
                            {currentModel}
                          </Badge>
                        </button>

                        {/* Clear button — only visible when there is a conversation */}
                        {messages.length > 0 && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={handleClear}
                            aria-label="Clear conversation"
                          >
                            <RotateCcw className="h-3 w-3" />
                          </Button>
                        )}
                      </Stack>
                      <Stack direction="horizontal" align="center" gap="snug">
                        {/*
                         * Keyboard hint — raw span: carries responsive utility
                         * (hidden sm:block) alongside text styling.
                         */}
                        {/* eslint-disable-next-line local/no-raw-div */}
                        <div className="hidden sm:block">
                          <Text size="micro" color="subtle">
                            Shift + Enter for new line
                          </Text>
                        </div>
                        {/*
                         * size="xs" shape="pill": compact pill send button.
                         */}
                        <Button size="xs" shape="pill" disabled={!canSubmit} onClick={handleSubmit}>
                          <Send className="h-3 w-3" />
                          Send
                        </Button>
                      </Stack>
                    </footer>
                  </Stack>
                </div>

                {/*
                 * mt-2 wrapper: spacing between the input card and the disclaimer.
                 */}
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="mt-2">
                  <Text size="micro" color="subtle" align="center">
                    Orbit AI can make mistakes. Verify important information before acting.
                  </Text>
                </div>
              </form>
            </Stack>
          </footer>
        </Stack>
      </section>
    </Stack>
  );
}
