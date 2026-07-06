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
window.__postedBodies = [];
window.__sockets = [];
window.__downloadClicks = [];
window.__uploadedFiles = [];
HTMLAnchorElement.prototype.click = function() {
  window.__downloadClicks.push({ href: this.href, download: this.download });
};
const servers = [
  { id: 1, name: 'harbor', ip: '192.168.1.133', port: 4500, username: 'root', group: 'prod', os_type: 'linux' },
  { id: 2, name: 'mysql', ip: '10.0.0.8', port: 22, username: 'deploy', group: 'prod', os_type: 'linux' },
  { id: 3, name: 'devbox', ip: '10.0.0.9', port: 2200, username: 'dev', group: 'dev', os_type: 'linux' },
];
window.fetch = function(url, opts) {
  url = String(url);
  if (url.indexOf('/api/client-log') >= 0) {
    return Promise.resolve({ ok: true, json: function() { return Promise.resolve({ ok: true }); } });
  }
  if (url.indexOf('/api/local-terminal/agents') >= 0) return Promise.resolve({ json: function() { return Promise.resolve({ agents: [] }); } });
  if (url.indexOf('/api/servers/') >= 0 && url.indexOf('/full') >= 0) {
    var id = Number(url.match(/\\/api\\/servers\\/(\\d+)\\/full/)[1]);
    var found = servers.find(function(server) { return server.id === id; });
    var full = Object.assign({ password: 'pw' + id }, found);
    if (id === 1) {
      full.jump_chain = [
        { id: 9, name: 'jump-a', ip: '172.16.0.10', port: 2222, username: 'jump', password: 'jump-pw' },
      ];
    }
    return Promise.resolve({ json: function() { return Promise.resolve(full); } });
  }
  if (url.indexOf('/api/servers') >= 0) return Promise.resolve({ json: function() { return Promise.resolve({ servers: servers }); } });
  if (url.indexOf('/api/files/1/read') >= 0) {
    return Promise.resolve({ ok: true, json: function() { return Promise.resolve({ path: '/etc/hosts', content: '127.0.0.1 localhost' }); } });
  }
  if (url.indexOf('/api/files/1/upload') >= 0) {
    window.__uploadedFiles.push({
      path: opts.body.get('path'),
      filename: opts.body.get('file').name,
    });
    return Promise.resolve({ ok: true, json: function() { return Promise.resolve({ path: opts.body.get('path') }); } });
  }
  if (url.indexOf('/api/files/1') >= 0) {
    var parsedFileUrl = new URL(url, 'http://local-terminal.test');
    var filePath = parsedFileUrl.searchParams.get('path') || '/';
    return Promise.resolve({ ok: true, json: function() { return Promise.resolve({
      path: filePath,
      files: filePath === '/'
        ? [{ name: 'etc', path: '/etc', is_dir: true }, { name: 'hosts', path: '/hosts', is_dir: false, size: 128 }]
        : [{ name: 'hosts', path: '/etc/hosts', is_dir: false, size: 128 }],
    }); } });
  }
  if (opts && opts.method === 'POST') {
    window.__postedBodies.push(opts.body ? JSON.parse(opts.body) : {});
    return Promise.resolve({ json: function() { return Promise.resolve({ id: 'session-' + (++window.__sid) }); } });
  }
  return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [] }); } });
};
window.WebSocket = function() {
  this.readyState = 1;
  this.__id = window.__sockets.length;
  window.__sockets.push(this);
  setTimeout(() => this.onopen && this.onopen(), 0);
};
window.WebSocket.prototype.send = function(data) { window.__sentData.push({ socket: this.__id, data }); };
window.WebSocket.prototype.close = function() { this.readyState = 3; if (this.onclose) this.onclose(); };
window.Terminal = function() {
  this.cols = 80;
  this.rows = 24;
  this.options = {};
  this.buffer = { active: { cursorX: 0, cursorY: 0, length: 0, getLine: function() { return null; } } };
  this.parser = { registerOscHandler: function() {} };
  window.__lastTerminal = this;
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
window.Terminal.prototype.attachCustomKeyEventHandler = function(handler) { this.__keyHandler = handler; };
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
    await page.goto('http://local-terminal.test/?token=test-token', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 1, null, { timeout: 5000 })
      .catch((err) => {
        if (pageErrors.length) throw new Error(pageErrors.join('\\n'));
        throw err;
      });

    assert.strictEqual(await page.locator('#server-sidebar.collapsed').count(), 1, 'server tree should be collapsed by default');
    await page.click('#server-btn');
    await page.waitForSelector('#server-sidebar:not(.collapsed)');
    await page.waitForSelector('[data-server-id="1"]');
    assert.ok((await page.textContent('#server-sidebar')).includes('prod'), 'server tree should group servers by tag/group');

    await page.fill('#server-search-input', 'har');
    assert.strictEqual(await page.locator('[data-server-id="1"]').count(), 1, 'matching server should remain visible after search');
    assert.strictEqual(await page.locator('[data-server-id="2"]').count(), 0, 'non-matching server should be hidden after search');

    await page.click('[data-server-id="1"]');
    await page.waitForFunction(() => window.__sentData.some((entry) => entry.data === 'ssh -o StrictHostKeyChecking=accept-new -J jump@172.16.0.10:2222 -p 4500 root@192.168.1.133\r'));
    let tabText = await page.evaluate(() => document.querySelector('.tab.active').childNodes[0].textContent);
    assert.strictEqual(tabText, 'harbor', 'connecting from server tree should name the tab after the server');

    await page.click('.pane-file-btn');
    await page.waitForSelector('#remote-file-panel.open');
    await page.waitForSelector('#remote-file-panel [data-file-path="/etc"]');
    assert.ok((await page.textContent('#remote-file-panel')).includes('harbor'), 'file panel should show the bound server name');
    await page.dblclick('#remote-file-panel [data-file-path="/etc"]');
    await page.waitForSelector('#remote-file-panel [data-file-path="/etc/hosts"]');
    await page.click('#remote-file-panel [data-file-path="/etc/hosts"]');
    await page.click('#remote-file-download');
    assert.strictEqual(
      await page.evaluate(() => decodeURIComponent(new URL(window.__downloadClicks[0].href).searchParams.get('path'))),
      '/etc/hosts',
      'download should target the selected remote file'
    );
    await page.setInputFiles('#remote-file-upload-input', {
      name: 'new.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('hello upload'),
    });
    await page.waitForFunction(() => window.__uploadedFiles.some((file) => file.path === '/etc/new.txt' && file.filename === 'new.txt'));
    await page.dblclick('#remote-file-panel [data-file-path="/etc/hosts"]');
    await page.waitForFunction(() => document.querySelector('#remote-file-preview').textContent.indexOf('127.0.0.1 localhost') >= 0);
    await page.click('#file-btn');
    await page.waitForFunction(() => !document.getElementById('remote-file-panel').classList.contains('open'));

    await page.evaluate(() => window.__sentData = []);
    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 2);
    await page.waitForFunction(() => window.__sentData.some((entry) => entry.data === 'ssh -o StrictHostKeyChecking=accept-new -J jump@172.16.0.10:2222 -p 4500 root@192.168.1.133\r'));

    await page.click('.pane-server-btn');
    await page.waitForSelector('#server-picker-menu [data-server-id="3"]');
    await page.fill('#server-picker-search', 'dev');
    await page.click('#server-picker-menu [data-server-id="3"]');
    await page.waitForFunction(() => window.__sentData.some((entry) => entry.data === 'ssh -o StrictHostKeyChecking=accept-new -p 2200 dev@10.0.0.9\r'));
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
