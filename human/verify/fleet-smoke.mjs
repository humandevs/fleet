// Fleet fork verification smoke test.
//
// Logs into a running Fleet dev server and screenshots key pages. Today it verifies base Fleet (login →
// dashboard → host list). Once we build the coverage matrix + coverage filter (RFC §5/§6), add steps here
// to open them and assert the cells/filters render — this harness is how we "see" the frontend without a
// local browser stack.
//
// Runs on the Windows HOST (Node is here) against a Fleet server in the Hyper-V VM. Fleet's dev server uses
// a self-signed cert, so we accept insecure certs.
//
// Usage (PowerShell or bash):
//   FLEET_URL=https://<vm-ip>:8080 FLEET_USER=admin@example.com FLEET_PASS='Fleet1234!' npm run smoke
// Defaults target https://localhost:8080 (use SSH/port-forward, or set FLEET_URL to the VM IP).

import puppeteer from "puppeteer";
import { mkdir } from "node:fs/promises";

const URL = process.env.FLEET_URL ?? "https://localhost:8080";
const USER = process.env.FLEET_USER ?? "admin@example.com";
const PASS = process.env.FLEET_PASS ?? "Fleet1234!";
const HOST_ID = process.env.FLEET_HOST_ID ?? "1"; // a host to open for the Coverage card
const OUT = "screenshots";

async function shot(page, name) {
  const path = `${OUT}/${name}.png`;
  await page.screenshot({ path, fullPage: true });
  console.log(`  📸 ${path}`);
}

async function main() {
  await mkdir(OUT, { recursive: true });
  console.log(`→ target: ${URL}`);

  const browser = await puppeteer.launch({
    headless: true,
    acceptInsecureCerts: true, // Fleet dev self-signed cert
    args: ["--no-sandbox", "--ignore-certificate-errors"],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900 });

  try {
    await page.goto(`${URL}/login`, { waitUntil: "networkidle2", timeout: 30000 });
    await shot(page, "01-landing"); // capture whatever renders, even if selectors drift

    // Fleet login form uses name-based inputs.
    await page.waitForSelector('input[name="email"]', { timeout: 15000 });
    await page.type('input[name="email"]', USER, { delay: 10 });
    await page.type('input[name="password"]', PASS, { delay: 10 });
    await Promise.all([
      page.click('button[type="submit"]'),
      page.waitForNavigation({ waitUntil: "networkidle2", timeout: 30000 }).catch(() => {}),
    ]);
    await shot(page, "02-dashboard");

    await page.goto(`${URL}/hosts/manage`, { waitUntil: "networkidle2", timeout: 30000 });
    await shot(page, "03-hosts");

    // Host details → the Coverage card lives in the Details tab (renders only if a provider has reported
    // cells for this host; run a provider Collect first, or seed host_integration_status).
    await page.goto(`${URL}/hosts/${HOST_ID}`, { waitUntil: "networkidle2", timeout: 30000 });
    await shot(page, "04-host-details-coverage");

    console.log("✅ smoke complete");
  } catch (err) {
    await shot(page, "99-error").catch(() => {});
    console.error("❌ smoke failed:", err.message);
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
}

main();
