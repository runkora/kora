// Opens a browser window to sign in to GitHub and saves the session to
// .auth/github.json, for walkthroughs that start on a GitHub page. A person
// signs in, so no GitHub credential is stored anywhere but that file.
//
//   node github-login.mjs [url]
//
// The url is a page the walkthroughs open, runkora/kora by default. An
// organisation behind SAML SSO asks for its own sign-in on top of GitHub's,
// so the session is saved only once that page loads.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const dir = path.dirname(fileURLToPath(import.meta.url));
const stateFile = path.join(dir, ".auth", "github.json");
const target = new URL(process.argv[2] ?? "https://github.com/runkora/kora");

const browser = await chromium.launch({ headless: false });
const context = await browser.newContext({
  viewport: { width: 1280, height: 860 },
  storageState: fs.existsSync(stateFile) ? stateFile : undefined,
});
const page = await context.newPage();
await page.goto(target.href);
console.log(`Sign in to GitHub, and to the organisation's SSO if asked, until ${target.href} loads.`);

// The page is reached once the browser sits on it and GitHub renders the
// repository rather than a 404 or an SSO prompt.
for (;;) {
  const here = new URL(page.url());
  const loaded =
    here.pathname === target.pathname &&
    (await page.locator('meta[name="octolytics-dimension-repository_nwo"]').count()) > 0;
  if (loaded) break;
  // A sign-in sends the browser elsewhere; once GitHub is signed in, return
  // to the page so the SSO prompt for it appears.
  const signedIn = (await context.cookies("https://github.com")).some((c) => c.name === "logged_in" && c.value === "yes");
  if (signedIn && here.hostname === "github.com" && here.pathname === "/") await page.goto(target.href);
  await page.waitForTimeout(1000);
}
fs.mkdirSync(path.dirname(stateFile), { recursive: true });
await context.storageState({ path: stateFile });
fs.chmodSync(stateFile, 0o600);
console.log(`Saved ${path.relative(process.cwd(), stateFile)}`);
await browser.close();
