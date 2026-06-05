# Language and Framework Selection (Go & Next.js)

**Status:** Accepted
**Date:** 2026-02-16
**Deciders:** Architecture Team
**Technical Story:** Selection of programming languages for backend and frontend development

---

## Context

Orbit is being developed in an era where AI-assisted development is becoming the norm. The codebase will be maintained and extended by both human developers and AI coding assistants. Language and framework choices directly impact development velocity, code quality, maintainability, and the effectiveness of AI assistance.

### Background

The project requires:
- A high-performance backend for the control plane API
- A modern, type-safe frontend framework
- Strong tooling and ecosystem support
- Languages that enable AI assistants to generate correct, maintainable code

### Problem Statement

We need to select:
1. **Backend language**: What language should we use for the control plane API, policy engine, and event spine?
2. **Frontend framework**: What framework should we use for the operator dashboard?

These choices will define the developer experience, maintainability, and AI-assisted development effectiveness for years to come.

### Goals

- Maximize AI-assisted development effectiveness
- Ensure type safety and compile-time error detection
- Enable high performance for control plane workloads
- Provide excellent tooling and debugging experience
- Minimize runtime errors through language design
- Support long-term maintainability

### Non-Goals

- Choosing the "most popular" language
- Optimizing for familiarity over effectiveness
- Supporting multiple backend languages
- Using cutting-edge experimental languages

---

## Decision Drivers

* **AI-Assisted Development**: Language must enable AI to generate correct, idiomatic code
* **Error Handling**: Force explicit error handling to prevent silent failures
* **Type Safety**: Catch errors at compile-time, not runtime
* **Performance**: Control plane requires efficient resource usage
* **Simplicity**: Avoid language complexity that confuses both humans and AI
* **Ecosystem**: Strong libraries for auth, observability, and distributed systems
* **Tooling**: Excellent IDE support, debugging, and profiling

---

## Considered Options

### Backend Language Options

#### Option 1: Go

**Description:** Statically typed, compiled language designed for simplicity and concurrency

**Pros:**
- **Idiomatic by default**: One obvious way to do things - AI generates consistent code
- **Explicit error handling**: Forces `if err != nil` checks - prevents silent failures
- **No hidden control flow**: No exceptions, no magic - code does what it looks like it does
- **Fast compilation**: Sub-second builds enable tight feedback loops
- **Built-in concurrency**: Goroutines and channels are first-class primitives
- **Single binary deployment**: No runtime dependencies or complex packaging
- **Excellent stdlib**: Comprehensive standard library reduces external dependencies
- **Strong typing**: Catches errors at compile time
- **Simple syntax**: Easy for AI to parse and generate correctly
- **Great tooling**: gofmt, go vet, gopls, built-in profiling

**Cons:**
- Verbose error handling (though this is a feature for AI development)
- No generics in older versions (solved in Go 1.18+)
- Less expressive than some languages
- Limited functional programming features

#### Option 2: Rust

**Description:** Systems programming language focused on memory safety and concurrency

**Pros:**
- Extremely safe (prevents memory bugs, data races)
- Excellent performance
- Strong type system with advanced features
- Growing ecosystem

**Cons:**
- **Steep learning curve**: Borrow checker confuses AI assistants
- **Complex error messages**: Hard for AI to interpret and fix
- **Longer compilation times**: Slower feedback loops
- **Verbose syntax**: AI often generates invalid lifetime annotations
- **Smaller ecosystem**: Fewer mature libraries for web services

#### Option 3: TypeScript (Node.js)

**Description:** Typed superset of JavaScript for server-side development

**Pros:**
- Type safety with gradual typing
- Shared language with frontend
- Large ecosystem
- Familiar to many developers

**Cons:**
- **Runtime errors**: Type system can be bypassed, errors manifest at runtime
- **Performance**: Slower than compiled languages for compute-heavy tasks
- **Memory usage**: Higher baseline memory consumption
- **Callback hell**: Async patterns can be complex
- **Dependency sprawl**: npm packages often have deep dependency trees

### Frontend Framework Options

#### Option 1: Next.js (React)

**Description:** React framework with server-side rendering, routing, and API routes

**Pros:**
- **Industry standard**: Proven for production applications
- **Excellent DX**: Hot reload, great error messages, extensive documentation
- **TypeScript first-class**: Strong type safety in frontend stack
- **Built-in API routes**: Server-side execution for BFF pattern
- **Rich ecosystem**: Massive library selection for any need
- **AI-friendly**: Well-documented patterns AI can replicate
- **Server & client rendering**: Flexibility in rendering strategies
- **Great tooling**: Vercel deployment, extensive debugging tools

**Cons:**
- Framework lock-in
- Can be heavy for simple use cases
- Requires understanding React mental model

#### Option 2: Svelte/SvelteKit

**Description:** Compiled UI framework with built-in reactivity

**Pros:**
- Simpler mental model than React
- Excellent performance
- Less boilerplate code
- Growing ecosystem

**Cons:**
- **Smaller ecosystem**: Fewer libraries and resources
- **Less AI training data**: AI assistants less proficient with Svelte
- **Smaller community**: Harder to find solutions to problems
- **Newer framework**: Less battle-tested in production

#### Option 3: Vue/Nuxt

**Description:** Progressive JavaScript framework with official routing and SSR

**Pros:**
- Approachable learning curve
- Good documentation
- Strong ecosystem
- TypeScript support improving

**Cons:**
- **Less AI proficiency**: AI assistants better with React
- **Smaller ecosystem than React**: Fewer component libraries
- **Composition API complexity**: Multiple ways to do things

---

## Decision

**Chosen Options:**
- **Backend: Go**
- **Frontend: Next.js (React + TypeScript)**

### Rationale

#### Why Go for Backend

Go is **optimized for AI-assisted development** in ways that other languages are not:

1. **Idiomatic by design**: There's usually one obvious way to write Go code. AI assistants don't waste tokens exploring multiple coding styles - they generate consistent, idiomatic code every time.

2. **Verbosity forces correctness**: While some see `if err != nil` as tedious, it's actually a strength for AI development. The language forces explicit error handling, preventing the silent failures that plague exception-based languages. AI must handle errors, not ignore them.

3. **No hidden control flow**: Go has no exceptions, no operator overloading, no metaclasses. Code does exactly what it looks like it does. This makes it trivial for AI to reason about control flow and side effects.

4. **Fast feedback loops**: Sub-second compilation means AI can generate code, compile it, see errors, and fix them rapidly. This tight loop improves AI code quality significantly.

5. **Simple type system**: Go's type system is powerful enough to prevent common bugs but simple enough that AI rarely generates type errors. Compare this to Rust's borrow checker, which AI struggles with.

6. **Single binary deployment**: No runtime dependencies, no version conflicts, no complex packaging. Build once, deploy anywhere.

7. **Excellent for control planes**: Go was designed for systems like Kubernetes, etcd, and Docker. It's proven at scale for distributed systems.

**For a control plane built with AI assistance, Go is the optimal choice.**

#### Why Next.js for Frontend

1. **TypeScript type safety**: Catches errors at compile time, provides excellent autocomplete and refactoring support for both humans and AI.

2. **Massive AI training corpus**: Next.js/React code dominates GitHub and Stack Overflow. AI assistants are extremely proficient at generating correct Next.js code.

3. **Built-in BFF pattern**: API routes provide server-side execution for our BFF layer without additional services.

4. **Proven at scale**: Used by Vercel, Netflix, TikTok, and thousands of production applications.

5. **Excellent developer experience**: Hot reload, great error messages, extensive documentation make iteration fast.

6. **Rich ecosystem**: Need auth? Use NextAuth. Need forms? Use React Hook Form. Every problem has well-tested solutions.

---

## Consequences

### Positive

#### Go Backend
- AI generates idiomatic, correct Go code with minimal errors
- Explicit error handling prevents production bugs
- Fast compilation enables rapid iteration
- Single binary deployment simplifies operations
- Strong typing catches issues at compile time
- Excellent performance for control plane workloads
- Great tooling (profiling, tracing, debugging)

#### Next.js Frontend
- TypeScript provides type safety across frontend stack
- AI assistants excel at generating Next.js code
- Rich component ecosystem accelerates development
- Server-side rendering improves initial load performance
- API routes enable BFF pattern without extra services
- Hot reload makes UI iteration instantaneous

### Negative

#### Go Backend
- More verbose than some languages (though this is a feature for AI)
- Limited functional programming capabilities
- Less expressive than TypeScript for complex type relationships

#### Next.js Frontend
- Framework lock-in to React ecosystem
- Bundle size can be large for complex applications
- Requires understanding React's mental model

### Neutral

#### Go Backend
- Different language from frontend (but clear API boundary)
- Requires separate toolchain from frontend

#### Next.js Frontend
- Opinionated framework (but opinions are generally good)
- Requires Node.js runtime for development

---

## Implementation

### Go Setup
- Use Go 1.25 (latest) for cutting-edge features and performance
- Adopt standard project layout (`cmd/`, `pkg/`, `internal/`)
- Use `gofmt` and `golangci-lint` for code quality
- Leverage `go.mod` for dependency management

### Next.js Setup
- Use App Router (not Pages Router) for modern patterns
- Enable strict TypeScript mode
- Use ESLint and Prettier for code quality
- Leverage TypeScript for type safety

### Migration Path

N/A - This is the initial language selection.

---

## Validation

### Success Metrics

**AI Code Quality:**
- AI-generated Go code compiles on first attempt >80% of the time
- AI-generated Next.js components render correctly >90% of the time
- Error handling is explicit in 100% of Go code

**Developer Productivity:**
- Backend features go from idea to PR in <1 day average
- Frontend features go from design to PR in <1 day average
- Build + test cycle completes in <2 minutes

**Code Quality:**
- <5 production bugs per month related to language choice
- Code review feedback focuses on business logic, not syntax/idioms
- New developers productive within 1 week

### Monitoring

- Track AI code generation success rates
- Monitor build and test times
- Gather developer feedback on language effectiveness
- Track production bug root causes

---

## Related Decisions

- [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) - Monorepo structure using these languages
- Future ADR: API contract definition and type sharing between Go and TypeScript

---

## References

- [Go at Google: Language Design in the Service of Software Engineering](https://go.dev/talks/2012/splash.article)
- [Next.js Documentation](https://nextjs.org/docs)
- [Why Go is the Language of the Cloud](https://www.youtube.com/watch?v=rWJHbh6qO_Y)
- [AI-Assisted Development with Go](https://go.dev/blog/ai)

---

## Notes

**On Go's Verbosity:**
The explicit error handling in Go is often criticized as "boilerplate." However, for AI-assisted development, this verbosity is a feature, not a bug. It forces AI to consider error cases, prevents silent failures, and makes code behavior explicit and auditable.

**On Next.js Alternatives:**
While Svelte and Vue have their merits, the overwhelming amount of Next.js/React training data available to AI assistants makes them significantly more effective at generating correct frontend code. Developer productivity with AI assistance outweighs the minor performance or simplicity benefits of alternatives.

**Last Updated:** 2026-02-16