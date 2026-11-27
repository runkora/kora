// Signs in to kora once and saves the browser state, so every feature starts
// already authenticated and no recording shows the login screen. The saved
// state is reused while the session is still valid.
import fs from "node:fs";
import path from "node:path";

export async function signIn(browser, { baseURL, username, password, stateFile }) {
  if (fs.existsSync(stateFile) && (await stillSignedIn(browser, baseURL, stateFile))) {
    return stateFile;
  }

  const context = await browser.newContext({ baseURL, ignoreHTTPSErrors: true });
  try {
    const page = await context.newPage();
    await page.goto("/login");
    await page.getByRole("textbox").fill(username);
    await page.getByRole("button", { name: "Continue" }).click();

    // The email resolves to the tenant's identity provider. Locally that is
    // dex in front of an LDAP directory, with its own login form.
    await page.getByLabel("Username").waitFor({ timeout: 60_000 });
    await page.getByLabel("Username").fill(username);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Login" }).click();
    await page.waitForURL((url) => url.origin === new URL(baseURL).origin && !isAuthPath(url));

    fs.mkdirSync(path.dirname(stateFile), { recursive: true });
    await context.storageState({ path: stateFile });
    return stateFile;
  } finally {
    await context.close();
  }
}

async function stillSignedIn(browser, baseURL, stateFile) {
  const context = await browser.newContext({
    baseURL,
    ignoreHTTPSErrors: true,
    storageState: stateFile,
  });
  try {
    const res = await context.request.get("/api/auth/session");
    return res.ok() && Boolean((await res.json())?.user);
  } catch {
    return false;
  } finally {
    await context.close();
  }
}

function isAuthPath(url) {
  return url.pathname.startsWith("/login") || url.pathname.startsWith("/auth");
}
