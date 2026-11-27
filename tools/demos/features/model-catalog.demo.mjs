// A platform admin's view of the model gateway: the traffic it governs, the
// providers and models behind it, then a virtual model that serves Claude
// Opus 5 from Sydney and fails over to Oregon, as kora runs it in production.
// Needs the us, usw and aus Bedrock providers (scripts/cluster/
// refresh-bedrock-creds.sh with BEDROCK_ACCOUNTS=us,usw,aus).
import { feature } from "../lib/feature.mjs";

const NAME = "claude-opus-5";
const DISPLAY_NAME = "Claude Opus 5";
const DESCRIPTION = "Served from Sydney, failing over to Oregon when the Sydney route errors.";
const ROUTES = [
  { provider: "AWS Bedrock: aus", model: "Anthropic Claude Opus 5" },
  { provider: "AWS Bedrock: usw", model: "Anthropic Claude Opus 5" },
];

export default feature(
  {
    name: "model-catalog",
    title: "Govern every model call from one catalog",
    start: "/model-gateway/overview",
    reset,
  },
  async (demo) => {
    const { page } = demo;
    const panel = page.getByRole("dialog");

    await demo.caption("Every model call your agents make goes through one gateway");
    await demo.pause(3500);

    await demo.click(page.getByRole("link", { name: "Catalog" }));
    await page.getByRole("heading", { name: "Model Catalog" }).waitFor();
    await demo.caption("Providers across clouds and regions. Credentials never leave the gateway");
    await demo.click(page.getByRole("tab", { name: /^Providers/ }));
    await demo.pause(2000);

    await demo.caption("Every model each provider serves, with pricing kept in sync");
    await demo.click(page.getByRole("tab", { name: /^Provider Catalog/ }));
    await demo.type(page.getByRole("textbox", { name: "Search models…" }), "claude-opus-5");
    await demo.pause(2000);

    await demo.caption("Agents call virtual models. kora decides where each call goes");
    await demo.click(page.getByRole("tab", { name: /^Virtual Models/ }));
    await demo.click(page.getByRole("button", { name: "New Virtual Model" }));
    await panel.waitFor();

    const selects = panel.getByRole("combobox");
    await demo.select(selects.nth(0), "anthropic");
    await demo.type(panel.getByPlaceholder("e.g. claude-sonnet-4-6"), NAME);
    await demo.caption("It gets its own policy scope, so grants decide who may call it");
    await demo.hover(panel.getByText("Scope (derived)", { exact: true }));
    await demo.pause(1200);
    await demo.type(panel.getByPlaceholder("e.g. Claude Sonnet 4.6"), DISPLAY_NAME);
    await demo.type(panel.getByPlaceholder("Optional description"), DESCRIPTION);

    await demo.caption("Sydney first. If it fails, the gateway moves to Oregon");
    await demo.hover(panel.getByRole("radio", { name: "Priority / Failover" }));
    for (const [i, route] of ROUTES.entries()) {
      await demo.click(panel.getByRole("button", { name: "Add Route" }), { room: true });
      await demo.select(selects.nth(1 + i * 2), route.provider);
      await demo.select(selects.nth(2 + i * 2), route.model);
    }
    await demo.pause(1500);
    await demo.click(panel.getByRole("button", { name: "Save Changes" }));
    await panel.waitFor({ state: "hidden" });

    await demo.caption("Live for every agent with access, with no gateway restart");
    const row = page.getByRole("row", { name: new RegExp(NAME) });
    await row.waitFor();
    await demo.hover(row);
    await demo.pause(1500);
    await demo.click(page.getByRole("link", { name: "Models", exact: true }));
    await page.getByText(NAME, { exact: true }).waitFor();
    await demo.hover(page.getByText(NAME, { exact: true }));
    await demo.caption();
    await demo.pause(2500);
  },
);

async function reset(api) {
  const res = await api.get("/api/catalog/virtual-models");
  if (!res.ok()) throw new Error(`GET /api/catalog/virtual-models: ${res.status()}`);
  const body = await res.json();
  for (const model of body.data ?? body) {
    if (model.name !== NAME) continue;
    const deleted = await api.delete(`/api/catalog/virtual-models/${model.id}`);
    if (!deleted.ok()) throw new Error(`delete virtual model ${NAME}: ${deleted.status()}`);
  }
}
