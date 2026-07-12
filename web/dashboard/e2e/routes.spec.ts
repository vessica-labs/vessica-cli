import { expect, test, type Page } from "@playwright/test";

const envelope = (data: unknown) => ({ schema: "vessica.dashboard/v1", data });

async function mockDashboard(page: Page) {
  await page.route("**/auth/session", (route) =>
    route.fulfill({
      json: envelope({ user_id: "owner", role: "owner", mode: "local" }),
    }),
  );
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: unknown = {};
    if (path === "/api/v1/system") {
      data = {
        mode: "local",
        workspace_id: "ws",
        workspace_profile: "solo",
        version: "test",
        database: { status: "ready", backend: "sqlite" },
        knowledge: { status: "ready", retrieval_mode: "lexical" },
        integrations: [],
        counts: { runs: 0, sandboxes: 0 },
        warnings: [],
      };
    } else if (path === "/api/v1/runs" || path === "/api/v1/sandboxes") {
      data = { items: [] };
    } else if (path === "/api/v1/knowledge/status") {
      data = {
        retrieval_mode: "lexical",
        embedding_state: "not_configured",
        index_fresh: true,
      };
    } else if (path === "/api/v1/knowledge/search") {
      data = { items: [] };
    } else if (path === "/api/v1/docs") {
      data = [{ slug: "operator", title: "Operator guide", bytes: 2048 }];
    } else if (path === "/api/v1/access/members") {
      data = [];
    }
    return route.fulfill({ json: envelope(data) });
  });
}

test("primary dashboard routes expose deliberate empty and ready states", async ({
  page,
}) => {
  await mockDashboard(page);
  for (const [path, heading] of [
    ["/runs", "Runs"],
    ["/sandboxes", "Sandboxes"],
    ["/knowledge", "Knowledge explorer"],
    ["/docs", "Documentation"],
    ["/hosting", "Move to Railway"],
    ["/access", "Access"],
  ]) {
    await page.goto(path);
    await expect(
      page.getByRole("heading", { name: heading, exact: true }),
    ).toBeVisible();
  }
  const theme = page.locator(".sidebar .icon-button");
  await theme.click();
  await expect(theme).toHaveAttribute("aria-label", "Theme: light");
});
