import { chromium } from "playwright"
import assert from "node:assert/strict"

// Run against a disposable frontend: bun dev --hostname 127.0.0.1 --port 3107
// Then: node scripts/check-terminal-ui.mjs (or set JD_BROWSER_BASE_URL).
// Disposable API and socket fixtures: this check never reaches a host shell.
const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
const mutations = []
page.on("pageerror", (error) => errors.push(error.message))
let windows = ["codex", "server logs", "shell"].map((name, index) => ({
  index,
  name,
  active: index === 0,
  panes: 1,
  cwd: "/home/ubuntu/Just-Dashboard",
}))
await page.addInitScript(() => {
  localStorage.setItem(
    "jd.view.state",
    JSON.stringify({ "terminal.rail": false, "terminal.tools": false, "shell.sidebar": false }),
  )
})
await page.route("**/api/v1/**", async (route) => {
  const path = new URL(route.request().url()).pathname.replace("/api/v1", "")
  let data = {}
  if (route.request().method() === "POST") mutations.push(path)
  if (path === "/system/metrics")
    return route.fulfill({
      status: 503,
      json: { error: { code: "unavailable", message: "Preview" } },
    })
  if (path === "/system/health") data = { status: "ok" }
  if (path === "/auth/session")
    data = {
      authenticated: true,
      user: { username: "ubuntu", role: "admin" },
      capabilities: ["read", "terminal", "file.write"],
    }
  else if (path === "/terminal/")
    data = {
      enabled: true,
      tmux: true,
      login: { user: "ubuntu", home: "/home/ubuntu", shell: "/bin/bash" },
      folders: [],
      sessions: [
        {
          id: "preview",
          tmuxName: "preview",
          title: "Workspace",
          user: "ubuntu",
          live: true,
          persisted: true,
          windows: windows.length,
          cwd: "/home/ubuntu/Just-Dashboard",
          createdAt: new Date().toISOString(),
        },
      ],
    }
  else if (path.endsWith("/windows")) data = windows
  else if (/\/windows\/\d+$/.test(path) && route.request().method() === "PATCH") {
    const index = Number(path.split("/").at(-1))
    const body = route.request().postDataJSON()
    if (body.select) windows = windows.map((w) => ({ ...w, active: w.index === index }))
    if (body.name) windows = windows.map((w) => (w.index === index ? { ...w, name: body.name } : w))
  } else if (path.endsWith("/panes")) data = []
  else if (path === "/git/detect") data = { found: false }
  else if (path === "/files/list") data = { entries: [], path: "/home/ubuntu/Just-Dashboard" }
  await route.fulfill({ json: data })
})
await page.routeWebSocket("**/api/v1/**", (socket) => {
  if (socket.url().includes("/attach")) {
    socket.send(
      Buffer.from(
        "\x1b[32mubuntu\x1b[0m ~/Just-Dashboard\r\n$ git status\r\nOn branch main\r\nYour branch is up to date with origin/main.\r\n\r\nnothing to commit, working tree clean\r\n\r\n\x1b[32mubuntu\x1b[0m ~/Just-Dashboard\r\n$ ",
      ),
    )
    socket.onMessage((message) => {
      if (typeof message === "string" && !message.startsWith("{")) socket.send(Buffer.from(message))
    })
  }
})
try {
  await page.goto(`${process.env.JD_BROWSER_BASE_URL ?? "http://127.0.0.1:3107"}/terminal`)
  await page.locator(".xterm-screen").waitFor()
  const bar = page.locator('[aria-label="Terminal workspace"]')
  assert.equal(await bar.locator(":scope > button").count(), 2)
  assert.equal(await bar.getByText("/home/ubuntu/Just-Dashboard", { exact: true }).count(), 0)
  for (const tab of await page.locator("[data-window]").all()) {
    const box = await tab.boundingBox()
    assert(box.width >= 144 && box.height >= 44)
  }
  await page.getByRole("button", { name: "server logs", exact: true }).click()
  await page.locator('[data-window="1"][data-active="true"]').waitFor()
  await page.getByRole("button", { name: "More for window server logs" }).click()
  await page.getByRole("menuitem", { name: "Rename window", exact: true }).click()
  const rename = page.getByRole("textbox", { name: "Window name" })
  await rename.fill("build output")
  await rename.press("Enter")
  await page.getByRole("button", { name: "build output", exact: true }).waitFor()
  await page.getByRole("button", { name: "More for window codex" }).click()
  await page.getByRole("menuitem", { name: "Split side by side", exact: true }).click()
  await page.waitForFunction(() => !document.querySelector("[role=menu]"))
  assert(mutations.includes("/terminal/persistent/preview/windows/0/panes"))
  await page.getByRole("button", { name: "Terminal settings", exact: true }).click()
  await page.getByRole("button", { name: "Larger text", exact: true }).click()
  await page.keyboard.press("Escape")
  await page.getByRole("button", { name: "Terminal actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Save scrollback", exact: true }).waitFor()
  await page.keyboard.press("Escape")
  await page.getByRole("button", { name: /Search scrollback/ }).click()
  await page.getByRole("textbox", { name: "Find in scrollback" }).fill("branch")
  await page.getByRole("button", { name: "Close search", exact: true }).click()
  await page.getByRole("button", { name: "Show the sessions rail", exact: true }).click()
  await page.getByRole("button", { name: "Hide the sessions rail", exact: true }).click()
  await page.getByRole("button", { name: "Show files & git", exact: true }).click()
  await page.getByRole("button", { name: "Hide files & git", exact: true }).click()
  await page.getByRole("button", { name: "Show files & git", exact: true }).waitFor()
  await page.screenshot({ animations: "disabled", path: "/tmp/terminal-desktop.png" })
  await page.evaluate(() => {
    document.documentElement.className = "light"
    document.documentElement.style.colorScheme = "light"
    window.dispatchEvent(new Event("just-dashboard:themechange"))
  })
  await page.screenshot({ animations: "disabled", path: "/tmp/terminal-light.png" })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.locator('[data-window="2"] button').first().click()
  await page.locator('[data-window="2"][data-active="true"]').waitFor()
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
  await page.screenshot({ animations: "disabled", path: "/tmp/terminal-mobile.png" })
  assert.deepEqual(errors, [])
  console.log(
    "PASS: large tabs, two panel controls, switch/rename, actions, search, panel toggles, dark/light and mobile layout",
  )
} finally {
  await browser.close()
}
