# Marketplace accessibility and layout evidence

## Automated checks: 2026-10-09

Criterion 12's automated coverage lives in `marketplace.spec.ts`, in the
accessibility and layout `test.describe` group. It uses Playwright 1.62.1 and
headless Chromium 151.0.7922.34 against the actual Next.js application. The session
is supplied by the existing test cookie helper. Initial Marketplace data and
successful browse responses use the deterministic server fixture client. The
retry test controls the failed browser BFF response; browser preference writes
are intercepted. The commands below use an unreachable Orbit API URL so a live
Orbit backend is unnecessary.

| Scenario                             | Behavioral assertions                                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Toolbar keyboard flow                | Named search, Marketplace selector, sort and view controls are reached through Tab. Search changes results; Clear search recovers. The picker opens, dismisses with Escape, restores focus and changes selection through an arrow key. Keyboard sorting changes result order. Enter/Space switch between the same list/card results, with correct `aria-pressed` states; Shift+Tab returns focus. |
| Pagination keyboard flow             | Named page controls and Results per page are reached through Tab. Disabled Previous is skipped. Next, Previous and a numbered page change results through Enter/Space; `aria-current`, ranges and the disabled last-page Next are checked. A keyboard page-size change resets to the first page.                                                                                                  |
| Retry keyboard flow                  | A controlled failed request retains previous results. The named Try again button is reached through Tab and activated with Enter. The repeated request succeeds, replaces the results and removes the error notice.                                                                                                                                                                               |
| Toolbar at 1023 and 1024 px          | Browser geometry proves a vertical arrangement at 1023 px and a horizontal arrangement at 1024 px. Controls fit horizontally without overlap, card view works through the keyboard and the page does not overflow horizontally.                                                                                                                                                                   |
| Overview at 390 × 844 and 1440 × 900 | A populated middle page exercises pagination. List/card controls fit without overlap or page overflow; cards use one mobile column and two desktop columns.                                                                                                                                                                                                                                       |
| Detail at 390 × 844 and 1440 × 900   | Instructions and Source metadata render inside the screen. Source sits below Contents on mobile and beside it on desktop, with no overlap or page overflow.                                                                                                                                                                                                                                       |

The browser checks exposed two feature layout problems. Pagination's page-button
row now wraps within its container. Skill detail uses existing Terra Stack props
to place Source below Contents on mobile, retaining the `detail-panel` width on
desktop. No Terra implementation was modified.

## Commands and actual results

Run from `frontend/`:

```bash
PLAYWRIGHT_BASE_URL=http://localhost:3100 ORBIT_API_URL=http://127.0.0.1:9 \
  pnpm exec playwright test e2e/marketplace.spec.ts \
  --grep 'accessibility and layout' --workers=1 --reporter=line,html --timeout=60000

PLAYWRIGHT_BASE_URL=http://localhost:3100 ORBIT_API_URL=http://127.0.0.1:9 \
  pnpm exec playwright test e2e/marketplace.spec.ts \
  --workers=1 --reporter=line --timeout=60000

pnpm exec vitest run --config vitest.config.unit.ts \
  'src/app/(dashboard)/marketplace' \
  src/lib/routing/routes.test.ts src/lib/routing/api-routes.test.ts \
  --maxWorkers=2 --reporter=dot

pnpm exec vitest run --config vitest.config.integration.ts \
  'src/app/(dashboard)/marketplace' --maxWorkers=2 --reporter=dot

pnpm lint
pnpm tsc --noEmit
```

- Criterion 12 browser group: all 9 cases passed.
- Full Marketplace browser spec: exit 0, runner reports 14 passed. The original
  overview → detail → return case still uses `test.fail()`: 13 ordinary cases
  passed and that case matched its expected failure. This does not prove
  successful navigation and does not complete criterion 11.
- Marketplace/routing unit run: 9 files, 115 tests passed, exit 0.
- Marketplace integration run: 6 files, 61 tests passed, exit 0.
- TypeScript: passed, exit 0.
- Lint: exit 0, zero errors, the existing `src/lib/use-logout.ts:19` warning.
- `git diff --check`: passed.

Port 3000 was occupied by an unrelated process. Playwright's server command,
readiness URL and test-cookie URL now follow `PLAYWRIGHT_BASE_URL`; this run used
port 3100. The default remains `http://localhost:3000`.

Layout screenshots are attached to the HTML report generated in
`frontend/playwright-report/`. Open it from `frontend/` with:

```bash
pnpm exec playwright show-report
```

Only Chromium was exercised. Mobile checks resize a real browser viewport; no
physical mobile device or other browser engine was checked. Full frontend test
suites, other browser specs, Storybook checks and root `make check` were not run
for this step. No commit or PR was created. Run the required root quality gate
before committing.

## Manual screen-reader checks: pending

No screen-reader assessment was performed. Automated name, focus and interaction
assertions do not establish what assistive technology announces.

| Check                                                                  | Actual result |
| ---------------------------------------------------------------------- | ------------- |
| Control names, roles and selected/current/disabled states              | Not performed |
| Picker focus, dismissal and return to its trigger                      | Not performed |
| Search results and loading/error announcements                         | Not performed |
| Keyboard retry and recovery announcements                              | Not performed |
| Detail reading order, instructions, resource files and Source metadata | Not performed |

When these are performed, record the date, OS, browser and screen-reader versions,
tasks attempted, actual announcements, pass/fail findings and linked issues.
