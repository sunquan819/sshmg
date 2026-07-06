const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

function findBrowserExecutable() {
  const candidates = [
    process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  ].filter(Boolean);
  return candidates.find((candidate) => fs.existsSync(candidate));
}

async function main() {
  const templatePath = path.join(__dirname, 'local-terminal.html');
  let html = fs.readFileSync(templatePath, 'utf8');

  html = html
    .replace(/{{\.Desktop}}/g, 'true')
    .replace(/{{\.DesktopPort}}/g, '0')
    .replace(/<script src="[^"]+"><\/script>\s*/g, '');

  html = html.replace(
    '<script>\n(function() {',
    `<script>
window.__sid = 0;
window.__sentData = [];
window.__fetchUrls = [];
window.fetch = function(url, opts) {
  url = String(url);
  window.__fetchUrls.push(url);
  if (url.indexOf('/api/servers/7/full') >= 0) {
    return Promise.resolve({ ok: true, json: function() { return Promise.resolve({
      id: 7,
      name: 'prod-a',
      ip: '10.0.0.8',
      port: 2222,
      username: 'root',
      password: 's3cret!',
    }); } });
  }
  if (opts && opts.method === 'POST') {
    return Promise.resolve({ json: function() { return Promise.resolve({ id: 'session-' + (++window.__sid) }); } });
  }
  return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [] }); } });
};
window.WebSocket = function() {
  this.readyState = 1;
  window.__socket = this;
  setTimeout(() => this.onopen && this.onopen(), 0);
};
window.WebSocket.prototype.send = function(data) { window.__sentData.push(data); };
window.WebSocket.prototype.close = function() { this.readyState = 3; if (this.onclose) this.onclose(); };
window.Terminal = function() {
  this.cols = 80;
  this.rows = 24;
  this.options = {};
  this.buffer = { active: { cursorX: 0, cursorY: 0, length: 0, getLine: function() { return null; } } };
  this.parser = { registerOscHandler: function() {} };
};
window.Terminal.prototype.loadAddon = function() {};
window.Terminal.prototype.open = function(el) { var term = document.createElement('div'); term.className = 'xterm'; el.appendChild(term); this.element = term; };
window.Terminal.prototype.focus = function() {};
window.Terminal.prototype.dispose = function() {};
window.Terminal.prototype.writeln = function() {};
window.Terminal.prototype.write = function() {};
window.Terminal.prototype.onData = function(handler) { this.__onData = handler; };
window.Terminal.prototype.onResize = function(handler) { this.__resizeHandler = handler; };
window.Terminal.prototype.scrollLines = function() {};
window.Terminal.prototype.attachCustomKeyEventHandler = function() {};
window.Terminal.prototype.getSelection = function() { return ''; };
window.Terminal.prototype.clearSelection = function() {};
window.FitAddon = { FitAddon: function() { this.fit = function() {}; } };
window.SearchAddon = { SearchAddon: function() {} };
localStorage.removeItem('terminal-sessions');
(function() {`
  );

  const executablePath = findBrowserExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  const page = await browser.newPage();
  const pageErrors = [];
  page.on('pageerror', (err) => pageErrors.push(err.message));
  try {
    await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
    await page.goto('http://local-terminal.test/?token=test-token&startupServerId=7&startupTitle=prod-a', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => window.__sentData.includes('ssh -o StrictHostKeyChecking=accept-new -p 2222 root@10.0.0.8\r'), null, { timeout: 5000 })
      .catch((err) => {
        if (pageErrors.length) throw new Error(pageErrors.join('\\n'));
        return page.evaluate(() => ({
          sentData: window.__sentData,
          fetchUrls: window.__fetchUrls,
          body: document.body.textContent,
          tabCount: document.querySelectorAll('.tab').length,
        })).then((state) => {
          throw new Error(err.message + '\\n' + JSON.stringify(state, null, 2));
        });
      });

    await page.evaluate(() => window.__socket.onmessage({ data: "root@10.0.0.8's password: " }));
    await page.waitForFunction(() => window.__sentData.includes('s3cret!\r'), null, { timeout: 5000 });

    const state = await page.evaluate(() => ({
      fetchUrls: window.__fetchUrls,
      tabText: document.querySelector('.tab.active').childNodes[0].textContent,
      sentData: window.__sentData,
      savedSessions: localStorage.getItem('terminal-sessions'),
    }));
    assert.ok(state.fetchUrls.some((url) => url.indexOf('/api/servers/7/full') >= 0), 'startup mode should fetch full server credentials');
    assert.strictEqual(state.tabText, 'prod-a', 'startup server title should be used for the terminal tab');
    assert.strictEqual(state.sentData.filter((data) => data === 's3cret!\r').length, 1, 'password should be sent exactly once');
    assert.strictEqual(state.savedSessions, null, 'server startup terminal should not overwrite local terminal restore sessions');

    await page.evaluate(() => {
      window.__sentData = [];
      window.dispatchEvent(new MessageEvent('message', {
        data: { type: 'open-server-terminal', serverId: 7, title: 'prod-a again' },
      }));
    });
    await page.waitForFunction(() => document.querySelectorAll('.tab').length >= 2, null, { timeout: 5000 });
    await page.waitForFunction(() => window.__sentData.includes('ssh -o StrictHostKeyChecking=accept-new -p 2222 root@10.0.0.8\r'), null, { timeout: 5000 });
    const afterMessage = await page.evaluate(() => ({
      paneCount: document.querySelectorAll('.pane-item').length,
      tabTitles: Array.from(document.querySelectorAll('.tab')).map((tab) => tab.childNodes[0].textContent),
    }));
    assert.strictEqual(afterMessage.paneCount, 2, 'message launch should append a new server terminal tab');
    assert.deepStrictEqual(afterMessage.tabTitles, ['prod-a', 'prod-a'], 'message launch should keep the first server terminal and add another tab');

    await page.evaluate(() => {
      window.__sentData = [];
      window.fetch = function(url, opts) {
        url = String(url);
        window.__fetchUrls.push(url);
        if (url.indexOf('/api/servers/8/full') >= 0) {
          return Promise.resolve({ ok: true, json: function() { return Promise.resolve({
            id: 8,
            name: 'mysql',
            ip: '10.0.0.9',
            port: 2200,
            username: 'deploy',
            password: 'mysqlpw',
          }); } });
        }
        if (url.indexOf('/api/servers/7/full') >= 0) {
          return Promise.resolve({ ok: true, json: function() { return Promise.resolve({
            id: 7,
            name: 'prod-a',
            ip: '10.0.0.8',
            port: 2222,
            username: 'root',
            password: 's3cret!',
          }); } });
        }
        if (opts && opts.method === 'POST') {
          return Promise.resolve({ json: function() { return Promise.resolve({ id: 'session-' + (++window.__sid) }); } });
        }
        return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [] }); } });
      };
      window.dispatchEvent(new MessageEvent('message', {
        data: { type: 'open-server-terminal', serverId: 8, title: 'mysql' },
      }));
    });
    await page.waitForFunction(() => document.querySelectorAll('.tab').length >= 3, null, { timeout: 5000 });
    await page.waitForFunction(() => window.__sentData.includes('ssh -o StrictHostKeyChecking=accept-new -p 2200 deploy@10.0.0.9\r'), null, { timeout: 5000 });
    const afterSecondServer = await page.evaluate(() => ({
      paneCount: document.querySelectorAll('.pane-item').length,
      tabTitles: Array.from(document.querySelectorAll('.tab')).map((tab) => tab.childNodes[0].textContent),
    }));
    assert.strictEqual(afterSecondServer.paneCount, 3, 'opening a different server should create a third terminal pane in a new tab');
    assert.deepStrictEqual(afterSecondServer.tabTitles, ['prod-a', 'prod-a', 'mysql'], 'different server launches should append tabs instead of replacing the first terminal');
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
