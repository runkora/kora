# kora Architecture: Diagrams and Flows

This document contains comprehensive Mermaid diagrams showing how kora's components interact.

---

## 1. System Architecture Overview

```mermaid
graph TB
    subgraph "Kubernetes Control Plane"
        K8s["Kubernetes API<br/>(CRDs, RBAC)"]
        SPIRE["SPIRE Server<br/>(Root CA)"]
    end

    subgraph "Agent Pod (with Sidecars)"
        Agent["Agent Process<br/>(Claude Code)"]
        Envoy["Envoy Proxy<br/>(Intercepts traffic)"]
        IdentityProxy["Identity Proxy<br/>(ext_proc handler)"]
        SPIFFECert["SPIFFE Certificate<br/>(/var/run/secrets/spiffe/)"]
    end

    subgraph "kora Control Plane Services"
        Identity["Identity Service<br/>:8080<br/>(Root of Trust)"]
        PolicySvc["Policy Service<br/>:8083<br/>(PAP/PDP)"]
        PolicyGW["Policy Gateway<br/>:8084<br/>(PEP)"]
        InferenceGW["Inference Gateway<br/>:8081<br/>(LLM Routing)"]
        KoraAgents["Kora Agents<br/>(Orchestration)"]
        Triggers["Triggers<br/>:8097<br/>(Webhook Ingestion)"]
        Skills["Skills Service<br/>:8098<br/>(Tool Discovery)"]
        SessionMgr["Session Manager<br/>:8090<br/>(Execution Context)"]
    end

    subgraph "Kubernetes Operator"
        Operator["Operator<br/>(CRD Reconciliation)"]
    end

    subgraph "External Systems"
        Database["Database<br/>(PostgreSQL)"]
        LLMProvider["LLM Provider<br/>(OpenAI, Anthropic)"]
        ExternalAPI["External API<br/>(GitHub, Stripe)"]
    end

    Agent -->|calls| Envoy
    Envoy -->|checks| PolicyGW
    Envoy -->|injects creds| IdentityProxy
    IdentityProxy -->|exchanges identity| Identity
    
    Envoy -->|uses cert| SPIFFECert
    SPIFFECert -->|verified by| Identity
    
    Operator -->|manages| K8s
    K8s -->|provisions certs| SPIRE
    SPIRE -->|issues| SPIFFECert
    
    PolicyGW -->|evaluates| PolicySvc
    PolicySvc -->|stores policies| Database
    
    KoraAgents -->|triggers| Triggers
    Triggers -->|creates runs| KoraAgents
    Triggers -->|stores| Database
    
    SessionMgr -->|runs| Agent
    Identity -->|issues credentials| SessionMgr
    
    Skills -->|caches| Database
    InferenceGW -->|tracks usage| Database
    
    Agent -->|calls| LLMProvider
    Agent -->|calls| Database
    Agent -->|calls| ExternalAPI
    
    style Agent fill:#e1f5ff
    style Envoy fill:#fff3e0
    style IdentityProxy fill:#fff3e0
    style Identity fill:#f3e5f5
    style PolicyGW fill:#f3e5f5
    style PolicySvc fill:#f3e5f5
```

---

## 2. Agent Authentication & SPIFFE Identity Flow

```mermaid
sequenceDiagram
    participant K8s as Kubernetes<br/>Scheduler
    participant SPIRE as SPIRE<br/>Server
    participant SPIREAgent as SPIRE<br/>Agent<br/>(on node)
    participant AgentPod as Agent Pod<br/>(with Envoy)
    participant Identity as Identity<br/>Service
    
    K8s->>AgentPod: Schedule pod on node<br/>(with ServiceAccount)
    
    rect rgb(200, 220, 255)
        note over SPIREAgent: SPIFFE Certificate Provisioning
        SPIREAgent->>K8s: Watch for new pods
        K8s-->>SPIREAgent: Pod detected<br/>(ServiceAccount: agent-xyz)
        SPIREAgent->>SPIREAgent: Verify pod credentials
        SPIREAgent->>SPIRE: Request certificate<br/>for spiffe://kora/ns/prod/sa/agent-xyz
    end
    
    SPIRE->>SPIRE: Sign X.509 certificate<br/>Validity: 1 hour
    SPIRE-->>SPIREAgent: Certificate + Private Key
    
    SPIREAgent->>AgentPod: Mount at<br/>/var/run/secrets/spiffe/
    
    rect rgb(220, 255, 220)
        note over AgentPod: Agent Startup
        AgentPod->>AgentPod: Envoy sidecar reads certificate
        AgentPod->>AgentPod: Start agent process
        AgentPod->>Envoy: "I have SPIFFE identity"
    end
    
    rect rgb(255, 240, 220)
        note over AgentPod: Later: Agent makes request
        AgentPod->>Envoy: GET https://api.example.com/data
        Envoy->>Envoy: Extract SPIFFE cert from<br/>/var/run/secrets/spiffe/cert.pem
        Envoy->>Identity: "Verify I am agent-xyz"
        Identity->>Identity: Verify SPIFFE signature
        Identity-->>Envoy: "Verified ✓"
    end
```

---

## 3. API Call with Policy Check & Credential Injection

```mermaid
sequenceDiagram
    participant Agent as Agent Code
    participant Envoy as Envoy Proxy<br/>Sidecar
    participant PolicyGW as Policy Gateway<br/>:8084
    participant PolicySvc as Policy Service<br/>:8083
    participant Identity as Identity Service<br/>:8080
    participant ExternalAPI as External API<br/>(e.g., GitHub)
    
    rect rgb(230, 245, 230)
        note over Agent,Envoy: Step 1: Agent makes request
        Agent->>Envoy: GET https://api.github.com/repos/user/repo
    end
    
    rect rgb(240, 250, 255)
        note over Envoy,PolicyGW: Step 2: Policy Check (Question 2: What can it access?)
        Envoy->>Envoy: Extract SPIFFE identity from cert
        Envoy->>PolicyGW: Can principal "agent-xyz"<br/>action "call_api"<br/>resource "https://api.github.com/repos/*"?
        
        PolicyGW->>PolicySvc: Evaluate policy bundle
        PolicySvc->>PolicySvc: Check OPA policy:<br/>agent-xyz has grant for this?
        PolicySvc-->>PolicyGW: ALLOW ✓
        
        PolicyGW-->>Envoy: ALLOW (decision logged)
    end
    
    rect rgb(255, 250, 240)
        note over Envoy,Identity: Step 3: Credential Injection (Question 3: How does it connect?)
        Envoy->>Identity: Exchange SPIFFE identity<br/>for credentials to api.github.com
        
        Identity->>Identity: Check: Agent authorized<br/>for GitHub API?
        Identity->>Identity: Get stored GitHub token<br/>(or refresh if expired)
        Identity-->>Envoy: GitHub token (temporary,<br/>expires in 1 hour)
    end
    
    rect rgb(245, 245, 255)
        note over Envoy,ExternalAPI: Step 4: Request with injected credentials
        Envoy->>Envoy: Inject token into request:<br/>Authorization: Bearer <token>
        Envoy->>ExternalAPI: GET /repos/user/repo<br/>Authorization: Bearer <token>
        
        ExternalAPI->>ExternalAPI: Validate token
        ExternalAPI-->>Envoy: 200 OK<br/>{repository data}
    end
    
    rect rgb(250, 240, 245)
        note over Envoy,Agent: Step 5: Response to agent
        Envoy-->>Agent: 200 OK<br/>{repository data}
        Agent->>Agent: Unaware of policy check<br/>or credential injection
    end
    
    rect rgb(240, 240, 240)
        note over Envoy: Logging (Everything is audited)
        Envoy->>Envoy: Log:<br/>- Principal: agent-xyz<br/>- Action: call_api<br/>- Resource: api.github.com<br/>- Decision: ALLOW<br/>- Status: 200
    end
```

---

## 4. Complete Message Sequence: Webhook Trigger → Agent Execution

```mermaid
sequenceDiagram
    participant GithubWebhook as GitHub<br/>(External)
    participant Triggers as Triggers Service<br/>:8097
    participant DB as Database<br/>(PostgreSQL)
    participant KoraAgents as Kora Agents
    participant SessionMgr as Session Manager<br/>:8090
    participant AgentPod as Agent Pod<br/>(Claude Code)
    participant ExternalAPI as External API<br/>(GitHub)
    
    rect rgb(255, 240, 240)
        note over GithubWebhook,Triggers: 1. Webhook Arrives
        GithubWebhook->>Triggers: POST /v1/webhooks/abc123<br/>{event: "pull_request", action: "opened", ...}
        Triggers->>Triggers: Authenticate webhook<br/>(verify signature)
    end
    
    rect rgb(245, 245, 255)
        note over Triggers,DB: 2. Durable Queue (Exactly-Once Delivery)
        Triggers->>DB: Create Delivery record<br/>(webhook_id, event_id, timestamp)
        Triggers->>DB: Lock row FOR UPDATE SKIP LOCKED
        DB-->>Triggers: Delivery row (claimed)
    end
    
    rect rgb(240, 250, 240)
        note over Triggers,KoraAgents: 3. Route to Target Agent
        Triggers->>Triggers: Look up trigger definition:<br/>"When PR opened → run code-review-agent"
        Triggers->>KoraAgents: Create run request<br/>(agent_id: code-review-agent,<br/>input: {pr_number: 123, ...})
    end
    
    rect rgb(250, 240, 250)
        note over KoraAgents,SessionMgr: 4. Orchestrate Execution
        KoraAgents->>DB: Create Run record<br/>(agent_id, trigger_id, status: queued)
        KoraAgents->>SessionMgr: Execute run:<br/>agent_id: code-review-agent<br/>input: {pr_number: 123}
    end
    
    rect rgb(240, 245, 255)
        note over SessionMgr,AgentPod: 5. Start Agent Pod
        SessionMgr->>SessionMgr: Allocate resources
        SessionMgr->>SessionMgr: Set up environment:<br/>- Input data<br/>- Credentials<br/>- Working directory
        SessionMgr->>AgentPod: Execute agent code
        AgentPod->>AgentPod: Agent process starts
    end
    
    rect rgb(230, 245, 230)
        note over AgentPod,ExternalAPI: 6. Agent Calls External API
        AgentPod->>AgentPod: Read PR number from input
        AgentPod->>AgentPod: Code: get_pr_details(123)
        AgentPod->>ExternalAPI: GET /repos/user/repo/pulls/123
        
        note over AgentPod: Envoy intercepts call:<br/>- Extracts SPIFFE identity<br/>- Checks policy<br/>- Injects GitHub token
        
        ExternalAPI-->>AgentPod: 200 OK {pr details}
    end
    
    rect rgb(245, 245, 220)
        note over AgentPod,SessionMgr: 7. Agent Completes
        AgentPod->>AgentPod: Process PR<br/>Generate review
        AgentPod->>ExternalAPI: POST /repos/.../pulls/123/reviews<br/>{comments}
        ExternalAPI-->>AgentPod: 200 OK
        
        AgentPod->>SessionMgr: exit(0)
    end
    
    rect rgb(240, 250, 240)
        note over SessionMgr,KoraAgents: 8. Report Result
        SessionMgr->>KoraAgents: Run completed<br/>status: success<br/>output: {review_url}
        KoraAgents->>DB: Update Run record<br/>status: completed
    end
    
    rect rgb(245, 240, 245)
        note over Triggers,DB: 9. Mark Delivery Complete
        Triggers->>DB: Update Delivery record<br/>status: delivered<br/>completed_at: now()
        Triggers->>DB: Commit transaction<br/>(release lock)
    end
```

---

## 5. Policy Evaluation Flow

```mermaid
graph LR
    subgraph "Request Arrives"
        A["Envoy receives<br/>outbound request"]
    end
    
    subgraph "Identity Extraction"
        B["Read SPIFFE certificate<br/>from /var/run/secrets/spiffe/"]
        C["Extract principal:<br/>agent-xyz"]
    end
    
    subgraph "Policy Evaluation"
        D{["Is policy<br/>embedded in<br/>Envoy?"]}
        E["Evaluate policy<br/>locally in Envoy<br/>(fast)"]
        F["Call Policy Gateway<br/>:8084<br/>(slower)"]
    end
    
    subgraph "OPA Policy Engine"
        G["Load OPA bundle<br/>from Policy Service"]
        H["Check grants:<br/>Does agent-xyz<br/>have permission X?"]
        I["Check roles:<br/>Does agent-xyz's role<br/>include permission X?"]
        J["Check scopes:<br/>Is permission<br/>still valid?"]
    end
    
    subgraph "Decision"
        K{["Allow or<br/>Deny?"]}
        L["ALLOW:<br/>Proceed to<br/>credential injection"]
        M["DENY:<br/>Return error<br/>to agent"]
    end
    
    subgraph "Logging"
        N["Log decision:<br/>- Principal<br/>- Action<br/>- Resource<br/>- Decision<br/>- Timestamp"]
    end
    
    A --> B --> C --> D
    D -->|Yes| E
    D -->|No| F
    E --> H
    F --> G --> H
    H --> I --> J --> K
    K -->|Allow| L
    K -->|Deny| M
    L --> N
    M --> N
    
    style A fill:#e3f2fd
    style E fill:#fff9c4
    style F fill:#fff9c4
    style H fill:#f3e5f5
    style I fill:#f3e5f5
    style J fill:#f3e5f5
    style L fill:#c8e6c9
    style M fill:#ffcdd2
    style N fill:#eeeeee
```

---

## 6. Credential Brokering & Exchange Flow

```mermaid
sequenceDiagram
    participant Agent as Agent Code
    participant Envoy as Envoy Proxy
    participant IdentityProxy as Identity Proxy<br/>(ext_proc)
    participant Identity as Identity Service<br/>:8080
    participant Vault as Vault<br/>(Secret Store)
    participant ExternalDB as External Database
    
    rect rgb(230, 245, 230)
        note over Agent,Envoy: Step 1: Agent connects to database
        Agent->>Agent: require 'database'<br/>db.connect("host=db.example.com")
        Agent->>Envoy: TCP connect to db.example.com:5432
    end
    
    rect rgb(240, 245, 255)
        note over Envoy,IdentityProxy: Step 2: Envoy intercepts
        Envoy->>Envoy: Intercept outbound connection
        Envoy->>IdentityProxy: Process request via ext_proc
    end
    
    rect rgb(255, 245, 240)
        note over IdentityProxy,Identity: Step 3: Identity Proxy asks for credentials
        IdentityProxy->>IdentityProxy: Read SPIFFE cert
        IdentityProxy->>Identity: Exchange request:<br/>principal: agent-xyz<br/>resource: db.example.com:5432<br/>scope: database_read
    end
    
    rect rgb(245, 240, 255)
        note over Identity,Vault: Step 4: Identity Service fetches credentials
        Identity->>Identity: Check policy:<br/>Can agent-xyz access db.example.com?
        Identity->>Vault: Get database credentials<br/>for agent_xyz
        Vault->>Vault: Generate temporary credentials<br/>user: agent_xyz_temp<br/>password: <random_pwd><br/>valid_until: now() + 1hour
        Vault-->>Identity: Credentials
    end
    
    rect rgb(250, 245, 240)
        note over Identity,IdentityProxy: Step 5: Return credentials
        Identity-->>IdentityProxy: Credentials:<br/>{username, password, expires_at}
    end
    
    rect rgb(240, 255, 245)
        note over IdentityProxy,ExternalDB: Step 6: Inject credentials
        IdentityProxy->>IdentityProxy: Inject username/password<br/>into PostgreSQL handshake
        Envoy->>ExternalDB: TCP connect + AUTH<br/>(injected credentials)
        ExternalDB->>ExternalDB: Validate credentials
        ExternalDB-->>Envoy: Connection accepted
    end
    
    rect rgb(230, 245, 230)
        note over Envoy,Agent: Step 7: Agent gets connection
        Envoy-->>Agent: Connected socket
        Agent->>Agent: Unaware credentials were injected
        Agent->>ExternalDB: SELECT * FROM users
    end
    
    rect rgb(240, 240, 240)
        note over IdentityProxy,Vault: Step 8: Cleanup (automatic)
        Note over IdentityProxy: After 1 hour expires:
        IdentityProxy->>Vault: Revoke temporary credentials
        Vault->>Vault: Delete user agent_xyz_temp
    end
```

---

## 7. Multi-Tenant Isolation

```mermaid
graph TB
    subgraph "Kubernetes Cluster"
        subgraph "Tenant-A Namespace"
            Tenant_A_Agent["Agent Pod<br/>(ServiceAccount: tenant-a)"]
            Tenant_A_SM["Session Manager<br/>for Tenant-A"]
        end
        
        subgraph "Tenant-B Namespace"
            Tenant_B_Agent["Agent Pod<br/>(ServiceAccount: tenant-b)"]
            Tenant_B_SM["Session Manager<br/>for Tenant-B"]
        end
        
        subgraph "kora Namespace"
            Identity["Identity Service<br/>:8080<br/>(Shared)"]
            PolicyGW["Policy Gateway<br/>:8084<br/>(Shared)"]
            DB["PostgreSQL<br/>(Shared)"]
            Redis["Redis Cache<br/>(Shared)"]
        end
        
        SPIRE["SPIRE Server<br/>(Cluster-wide)"]
    end
    
    Tenant_A_Agent -->|SPIFFE cert| SPIRE
    Tenant_B_Agent -->|SPIFFE cert| SPIRE
    
    Tenant_A_Agent -->|Call with tenant-a identity| PolicyGW
    Tenant_B_Agent -->|Call with tenant-b identity| PolicyGW
    
    Tenant_A_Agent -->|Session isolation| Tenant_A_SM
    Tenant_B_Agent -->|Session isolation| Tenant_B_SM
    
    Tenant_A_SM -->|Can only run tenant-a agents| Tenant_A_Agent
    Tenant_B_SM -->|Can only run tenant-b agents| Tenant_B_Agent
    
    PolicyGW -->|Query with tenant context| DB
    Identity -->|Query with tenant context| DB
    
    Tenant_A_Agent -.->|Cannot call| Tenant_B_Agent
    Tenant_A_Agent -.->|Cannot see| Tenant_B_SM
    
    style Tenant_A_Agent fill:#e8f5e9
    style Tenant_B_Agent fill:#fce4ec
    style Tenant_A_SM fill:#e8f5e9
    style Tenant_B_SM fill:#fce4ec
    style Identity fill:#f3e5f5
    style PolicyGW fill:#f3e5f5
    style DB fill:#fffde7
    style SPIRE fill:#e0f2f1
```

---

## 8. Operator: CRD to Pod Provisioning Flow

```mermaid
graph LR
    subgraph "Git/Config"
        A["Agent CRD<br/>manifests.yaml"]
    end
    
    subgraph "Kubernetes API"
        B["Agent CRD<br/>created"]
        C["Operator watches<br/>Agent CRD"]
    end
    
    subgraph "Operator Logic"
        D["Parse Agent spec:<br/>- Image<br/>- Config<br/>- Triggers<br/>- Secrets"]
        E["Generate Pod spec:<br/>- ServiceAccount<br/>- Sidecars<br/>- Resource limits"]
        F["Create Pod<br/>via K8s API"]
    end
    
    subgraph "Pod Lifecycle"
        G["Pod scheduled<br/>on node"]
        H["SPIRE Agent detects pod<br/>Issues SPIFFE cert"]
        I["Envoy sidecar starts<br/>loads policies"]
        J["Agent process starts<br/>Ready to execute"]
    end
    
    subgraph "Runtime"
        K["Agent receives<br/>webhook events"]
        L["Kora Agents<br/>creates run"]
        M["Session Manager<br/>executes agent"]
        N["Agent completes<br/>Result stored"]
    end
    
    A --> B --> C --> D --> E --> F --> G --> H --> I --> J --> K --> L --> M --> N
```

---

## 9. Policy Decision: Allow vs Deny

```mermaid
sequenceDiagram
    participant Agent as Agent<br/>Code
    participant Envoy as Envoy
    participant PolicyGW as Policy<br/>Gateway
    participant PolicySvc as Policy<br/>Service
    participant DB as Database
    
    rect rgb(230, 245, 230)
        note over Agent,Envoy: Scenario 1: ALLOWED
        Agent->>Envoy: POST https://api.github.com/repos/user/repo
        Envoy->>PolicyGW: Query: Can agent-xyz call api.github.com?
        PolicyGW->>PolicySvc: Evaluate OPA
        PolicySvc->>DB: Check grants for agent-xyz
        DB-->>PolicySvc: Grant found: call_api on api.github.com/*
        PolicySvc-->>PolicyGW: ALLOW
        PolicyGW-->>Envoy: ALLOW (log to DB)
        Envoy->>Envoy: Inject credentials
        Envoy->>Agent: Proceed (credential injected)
        Agent->>Agent: Request succeeds
    end
    
    rect rgb(255, 245, 245)
        note over Agent,Envoy: Scenario 2: DENIED
        Agent->>Envoy: DELETE https://api.example.com/user/123
        Envoy->>PolicyGW: Query: Can agent-xyz delete from api.example.com?
        PolicyGW->>PolicySvc: Evaluate OPA
        PolicySvc->>DB: Check grants for agent-xyz
        DB-->>PolicySvc: NO grant for delete action
        PolicySvc-->>PolicyGW: DENY<br/>(reason: "no delete grant")
        PolicyGW-->>Envoy: DENY (log to DB)
        Envoy->>Agent: 403 Forbidden
        Agent->>Agent: Request fails
    end
    
    rect rgb(245, 245, 255)
        note over Agent,Envoy: Scenario 3: EXPIRED GRANT
        Agent->>Envoy: GET https://api.example.com/data
        Envoy->>PolicyGW: Query: Can agent-xyz read from api.example.com?
        PolicyGW->>PolicySvc: Evaluate OPA
        PolicySvc->>DB: Check grants for agent-xyz
        DB-->>PolicySvc: Grant found BUT<br/>expires_at < now()
        PolicySvc-->>PolicyGW: DENY<br/>(reason: "grant expired")
        PolicyGW-->>Envoy: DENY (log to DB)
        Envoy->>Agent: 403 Forbidden
        Agent->>Agent: Request fails
    end
```

---

## 10. Error Handling & Denied Request Flow

```mermaid
graph TD
    A["Request arrives<br/>at Envoy"]
    
    B{["Policy check?"]}
    C["Call Policy Gateway"]
    D{["Decision?"]}
    E["ALLOW: Continue"]
    F["DENY: Block request"]
    
    G["Generate error response"]
    H["Log denial:<br/>- Principal<br/>- Action<br/>- Resource<br/>- Reason<br/>- Timestamp"]
    
    I["Send to Observability<br/>System<br/>(Prometheus, Jaeger)"]
    J["Alert if pattern detected<br/>(brute-force, etc.)"]
    
    K["Return error to agent:<br/>403 Forbidden<br/>or<br/>Network timeout"]
    
    L["Agent handles error:<br/>- Retry?<br/>- Fail gracefully?<br/>- Log?"]
    
    A --> B
    B -->|Check| C
    C --> D
    D -->|Allow| E
    D -->|Deny| F
    E -->|OK| K
    F --> G --> H --> I --> J --> K --> L
    
    style A fill:#e3f2fd
    style E fill:#c8e6c9
    style F fill:#ffcdd2
    style G fill:#ffcdd2
    style H fill:#fffde7
    style I fill:#f3e5f5
    style K fill:#e0e0e0
    style L fill:#f5f5f5
```

---

## 11. Trigger Delivery State Machine

```mermaid
stateDiagram-v2
    [*] --> RECEIVED: Webhook arrives
    
    RECEIVED --> QUEUED: Store in durable queue<br/>(Postgres)
    
    QUEUED --> CLAIMED: Attempt delivery<br/>(FOR UPDATE SKIP LOCKED)
    
    CLAIMED --> EXECUTING: Create run<br/>in target agent
    
    EXECUTING --> SUCCESS: Run completes<br/>successfully
    EXECUTING --> FAILED: Run fails or<br/>times out
    
    SUCCESS --> COMPLETED: Mark delivered
    COMPLETED --> [*]
    
    FAILED --> RETRY_WAIT: Exponential backoff<br/>1s, 2s, 4s, 8s...
    RETRY_WAIT --> QUEUED: Retry delivery
    
    RETRY_WAIT --> DEAD_LETTER: Max retries<br/>exceeded
    DEAD_LETTER --> [*]
    
    note right of RECEIVED
        Event received from
        webhook sender
    end note
    
    note right of QUEUED
        Guarantees: If system
        crashes, delivery
        will be retried
    end note
    
    note right of CLAIMED
        Distributed lock:
        Only one executor
        claims this delivery
    end note
    
    note right of SUCCESS
        Event delivered
        to agent successfully
    end note
    
    note right of DEAD_LETTER
        Event moved to
        dead-letter queue
        for manual inspection
    end note
```

---

## 12. Complete Request Lifecycle with All Checks

```mermaid
graph TD
    Start["Agent code:<br/>api.call()"]
    
    S1["Envoy intercepts<br/>outbound request"]
    
    S2["Extract SPIFFE<br/>identity from cert"]
    
    S3["Policy Gateway<br/>evaluate policy"]
    
    S3_Result{["Policy<br/>Decision?"]}
    S3_Allow["ALLOW"]
    S3_Deny["DENY"]
    
    S4["Identity Proxy<br/>exchange credentials"]
    
    S4_Result{["Credentials<br/>available?"]}
    S4_Yes["Credentials found"]
    S4_No["No credentials"]
    
    S5["Inject credentials<br/>into request"]
    
    S6["Send to<br/>external system"]
    
    S7{["External<br/>response?"]}
    S7_OK["200 OK"]
    S7_Error["4xx/5xx Error"]
    
    S8["Return response<br/>to agent code"]
    
    DenyError["Return 403<br/>to agent"]
    
    CredError["Return 401<br/>to agent"]
    
    LogAll["Log:<br/>- Principal<br/>- Action<br/>- Resource<br/>- Decision<br/>- Status"]
    
    Start --> S1 --> S2 --> S3 --> S3_Result
    
    S3_Result -->|ALLOW| S3_Allow --> S4
    S3_Result -->|DENY| S3_Deny --> DenyError
    
    S4 --> S4_Result
    S4_Result -->|Yes| S4_Yes --> S5
    S4_Result -->|No| S4_No --> CredError
    
    S5 --> S6 --> S7
    S7 -->|Success| S7_OK --> S8
    S7 -->|Error| S7_Error --> S8
    
    DenyError --> LogAll
    CredError --> LogAll
    S8 --> LogAll
    
    LogAll --> End["Complete<br/>(audit trail created)"]
    
    style Start fill:#e3f2fd
    style S3_Allow fill:#c8e6c9
    style S3_Deny fill:#ffcdd2
    style S7_OK fill:#c8e6c9
    style S7_Error fill:#ffcdd2
    style DenyError fill:#ffcdd2
    style CredError fill:#ffcdd2
    style End fill:#e0e0e0
    style LogAll fill:#fffde7
```

---

## 13. Service-to-Service Communication Flow

```mermaid
graph LR
    subgraph "Service A"
        ServiceA["Service A Pod<br/>(with Envoy)"]
    end
    
    subgraph "Service Mesh (Istio)"
        EnvoyA["Envoy<br/>Sidecar"]
        mTLS["mTLS<br/>(Mutual TLS)"]
    end
    
    subgraph "Service B"
        ServiceB["Service B Pod<br/>(with Envoy)"]
    end
    
    SubgraphA["A wants to call B"]
    
    SubgraphA --> ServiceA
    ServiceA -->|Call B via localhost:8080| EnvoyA
    EnvoyA -->|Verify SPIFFE cert| mTLS
    mTLS -->|Encrypted channel<br/>with mutual auth| EnvoyA
    EnvoyA -->|Forward request<br/>to B's Envoy| ServiceB
    ServiceB -->|Receive request<br/>with A's identity| ServiceB
    ServiceB -->|Check policy:<br/>Can A call B?| ServiceB
    ServiceB -->|Response| ServiceB
    ServiceB -->|Return to A| EnvoyA
    EnvoyA -->|Return response| ServiceA
    
    style ServiceA fill:#e8f5e9
    style ServiceB fill:#fce4ec
    style mTLS fill:#fff3e0
    style EnvoyA fill:#fff3e0
```

---

## 14. Grant vs Role vs Policy Hierarchy

```mermaid
graph TB
    Policy["OPA Policy<br/>(Rego code)<br/>Answers: Allow or Deny?"]
    
    Role["Role<br/>Named permission<br/>collection<br/>e.g., data-analyst"]
    
    Grant["Grant<br/>Principal → Permission<br/>binding<br/>e.g., agent-xyz can call API"]
    
    Principal["Principal<br/>- Agent<br/>- User<br/>- Service"]
    
    Principal --> Grant
    Grant --> Role
    Role --> Grant
    
    Role --> Policy
    Grant --> Policy
    
    Policy --> Decision["Policy Decision<br/>ALLOW / DENY"]
    
    note right of Grant
        Direct assignment of
        permission to principal
        e.g., "agent-xyz
        can read_file /workspace/*"
    end note
    
    note right of Role
        Reusable collection
        of grants
        e.g., "data-analyst role
        includes: call_api,
        read_file"
    end note
    
    note right of Policy
        Logic that evaluates
        grants and roles
        in context
        Time-based, quota-based, etc.
    end note
    
    style Policy fill:#f3e5f5
    style Role fill:#e1f5fe
    style Grant fill:#fff9c4
    style Principal fill:#f0f4c3
    style Decision fill:#c8e6c9
```

---

## 15. Agent Pod Sidecar Architecture

```mermaid
graph TB
    subgraph "Agent Pod"
        subgraph "Containers"
            AgentContainer["Agent Container<br/>(Claude Code)<br/>- PID: 1"]
            EnvoyContainer["Envoy Container<br/>(sidecar)<br/>- Intercepts traffic"]
            IdentityProxyContainer["Identity Proxy<br/>(inside Envoy)"]
        end
        
        subgraph "Volumes"
            SPIFFEVol["SPIFFE Volume<br/>/var/run/secrets/spiffe/<br/>- cert.pem<br/>- key.pem"]
            ConfigVol["Config Volume<br/>ConfigMap<br/>- agent.yaml"]
            WorkspaceVol["Workspace Volume<br/>- /workspace"]
        end
    end
    
    AgentContainer -->|localhost:15000| EnvoyContainer
    AgentContainer -->|read config| ConfigVol
    AgentContainer -->|read/write| WorkspaceVol
    
    EnvoyContainer -->|read certificate| SPIFFEVol
    EnvoyContainer -->|ext_proc handler| IdentityProxyContainer
    
    IdentityProxyContainer -->|use identity| SPIFFEVol
    
    style AgentContainer fill:#e3f2fd
    style EnvoyContainer fill:#fff3e0
    style IdentityProxyContainer fill:#fff3e0
    style SPIFFEVol fill:#e0f2f1
    style ConfigVol fill:#f1f8e9
    style WorkspaceVol fill:#fce4ec
```

---

## Summary: The Three Questions in Diagram Form

```mermaid
graph TB
    Request["Request from Agent"]
    
    Q1["❓ Question 1:<br/>WHO IS THIS AGENT?<br/><br/>Answer: SPIFFE<br/>Certificate"]
    
    Q2["❓ Question 2:<br/>WHAT CAN IT ACCESS?<br/><br/>Answer: OPA Policy<br/>Decision"]
    
    Q3["❓ Question 3:<br/>HOW DOES IT CONNECT?<br/><br/>Answer: Injected<br/>Credentials"]
    
    Execution["✓ Action Approved<br/>✓ Credentials Injected<br/>✓ Call Proceeds"]
    
    Logging["📊 Everything Logged<br/>- Principal<br/>- Action<br/>- Resource<br/>- Decision<br/>- Timestamp"]
    
    Request --> Q1
    Q1 --> Q2
    Q2 --> Q3
    Q3 --> Execution
    Execution --> Logging
    
    style Request fill:#e3f2fd
    style Q1 fill:#f3e5f5
    style Q2 fill:#f3e5f5
    style Q3 fill:#f3e5f5
    style Execution fill:#c8e6c9
    style Logging fill:#fffde7
```

---

## Legend

- **Orange boxes** = Network/Transport layer (Envoy, proxies)
- **Purple boxes** = Policy/Security layer (Identity, Policy)
- **Blue boxes** = Data/Storage layer (Database, Redis)
- **Green boxes** = Success/Allow paths
- **Red boxes** = Failure/Deny paths
- **Yellow boxes** = Logging/Observability

