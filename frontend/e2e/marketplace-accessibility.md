# Marketplace accessibility, layout, and navigation evidence

## Current automation: 2026-10-16

`marketplace.spec.ts` exercises the actual Next.js overview and dedicated Skill detail routes in Chromium. Playwright starts the test-only `marketplace-upstream.mjs` HTTP server for Server Component requests. Browser browse calls and preference writes use route mocks. Both layers use `marketplace-fixtures.ts`; no live Go backend, database, Redis, or production fixture path is required.

The selected Marketplace overview is `/marketplace/{marketplaceSlug}`; other browse settings stay out of the address. Opening a row or card navigates to `/marketplace/{marketplaceSlug}/{skillName}`. Detail refresh reloads the same Skill, and the Skills link returns to that Marketplace's overview. The bare `/marketplace` entry resolves the saved or first Marketplace.

| Scenario                         | Behavioral assertions                                                                                                                                                                                                                                                                                    |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Browse and detail navigation     | Marketplace, search, page size, sort, and page changes keep a clean overview address. Opening a row navigates to the detail path without a browser detail BFF fetch. Reload retains that Skill, and the return link opens the overview.                                                                  |
| Direct detail link               | A direct detail address loads its requested Skill and remains on that detail after refresh.                                                                                                                                                                                                              |
| Missing and unavailable details  | Missing Skills show a separate not-found view with a return link. An upstream failure stays on its detail route and offers a server refresh retry.                                                                                                                                                       |
| Session expiry                   | The real middleware redirects an expired session to sign-in before rendering the overview.                                                                                                                                                                                                               |
| Toolbar keyboard flow            | Tab reaches named controls. Search and Clear search change results. The picker opens, dismisses with Escape, restores focus, and changes selection with arrow keys. Keyboard sorting changes result order. Enter/Space switch list/card views with `aria-pressed` states. Cards are native detail links. |
| Pagination keyboard flow         | Tab skips disabled Previous. Enter/Space activate Next, Previous, and numbered pages. Ranges, `aria-current`, disabled boundaries, and page-size reset are asserted.                                                                                                                                     |
| Browse retry keyboard flow       | A controlled failure retains previous results. Tab reaches Try again; Enter repeats the request, updates results, and removes the error notice.                                                                                                                                                          |
| Toolbar at 1023 and 1024 px      | Geometry checks vertical and horizontal arrangements, control overlap, and page overflow.                                                                                                                                                                                                                |
| Overview at 390 and 1440 px      | A populated middle page exercises pagination, controls, one/two-column cards, and horizontal overflow.                                                                                                                                                                                                   |
| Server detail at 390 and 1440 px | Instructions and Source metadata fit. Source sits below Contents on mobile and beside it on desktop without overlap or overflow.                                                                                                                                                                         |

The feature uses existing Hades Stack props for wrapping pagination and responsive detail layout. No Hades implementation change is required.

## Reproduce

Run from `frontend/`:

```bash
PLAYWRIGHT_BASE_URL=http://localhost:3100 ORBIT_API_URL=http://127.0.0.1:9 \
  pnpm exec playwright test e2e/marketplace.spec.ts --workers=2 --reporter=line

pnpm exec vitest run --config vitest.config.unit.ts \
  'src/app/(dashboard)/marketplace' \
  src/lib/routing/routes.test.ts src/server/marketplace-client.test.ts \
  --maxWorkers=2 --reporter=dot

pnpm exec vitest run --config vitest.config.integration.ts \
  'src/app/(dashboard)/marketplace' src/app/api/marketplace \
  'src/app/(dashboard)/_components/dashboard-header-breadcrumb.integration.test.tsx' \
  --maxWorkers=2 --reporter=dot

pnpm lint
pnpm tsc --noEmit
pnpm build-storybook
```

Port 3000 may belong to another process. The Next.js server and session cookie follow `PLAYWRIGHT_BASE_URL`; these checks use port 3100. The test-only upstream defaults to port 3101 and accepts a `PLAYWRIGHT_MARKETPLACE_PORT` override.

## Verification record

The routing restoration on 2026-10-16 passed all 16 ordinary Chromium cases, 114 focused unit tests, and 59 Marketplace integration tests. It also passed TypeScript, lint with zero errors and one unrelated warning, and the Storybook build. This supersedes the 2026-10-09 evidence that included an expected-failure navigation case. The restored spec has no expected-failure navigation marker.

The accompanying maintenance rerun is recorded below when complete. Counts here are dated evidence, not a requirement for future test totals.

Layout screenshots are attached to the HTML report in `frontend/playwright-report/`. Open it from `frontend/`:

```bash
pnpm exec playwright show-report
```

The browser run covers Chromium with desktop and resized mobile viewports. Physical devices and other browser engines remain unverified. Run the root quality gate before committing.

## Manual screen-reader checks: pending

No screen-reader assessment was performed. Automated names, focus, and interactions do not establish assistive-technology announcements.

| Check                                                                   | Actual result |
| ----------------------------------------------------------------------- | ------------- |
| Control names, roles, and selected/current/disabled states              | Not performed |
| Picker focus, dismissal, and return to its trigger                      | Not performed |
| Search results and loading/error announcements                          | Not performed |
| Keyboard retry and recovery announcements                               | Not performed |
| Detail reading order, instructions, resource files, and Source metadata | Not performed |

Record the date, OS, browser and screen-reader versions, tasks, actual announcements, findings, and linked issues when these checks are performed.
