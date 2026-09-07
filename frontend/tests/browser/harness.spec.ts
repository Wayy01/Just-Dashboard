import { expect, test } from "@playwright/test"

// C0 proves that the repository-owned runner, server startup and browser wiring
// work. Safety-critical deployment journeys are added with the C3 product UI.
test("loads the signed-out shell", async ({ page }) => {
  await page.goto("/login")

  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible()
  await expect(page.getByRole("textbox", { name: "Username", exact: true })).toBeVisible()
  await expect(page.getByRole("textbox", { name: "Password", exact: true })).toBeVisible()
})
