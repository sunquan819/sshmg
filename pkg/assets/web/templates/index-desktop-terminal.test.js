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

function renderTemplate({ desktop, port }) {
  let html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  html = html
    .replace(/{{if \.Desktop}}true{{else}}false{{end}}/g, desktop ? 'true' : 'false')
    .replace(/{{if \.DesktopPort}}{{\.DesktopPort}}{{else}}0{{end}}/g, String(port || 0))
    .replace(/<script defer src="[^"]+"><\/script>\s*/g, '')
    .replace('<script>', '<script>window.tailwind = {};');
  return html;
}

async function assertTerminalLaunch(page, html, expected) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token with spaces');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    window.__openedUrls = [];
    window.open = function(url) { window.__openedUrls.push(url); };
    window.__fetchUrls = [];
    window.fetch = function(url) {
      window.__fetchUrls.push(String(url));
      return Promise.resolve({
        ok: true,
        json: function() {
          return Promise.resolve({
            id: 2,
            name: 'prod shell',
            ip: '10.0.0.8',
            port: 2222,
            username: 'root',
            os_type: 'linux',
          });
        },
      });
    };
    const vm = app();
    await vm.openTerminalFull({ id: 2, name: 'prod shell', ip: '10.0.0.8', port: 2222, username: 'root', os_type: 'linux' });
    return {
      opened: window.__openedUrls[0] || '',
      fetchUrls: window.__fetchUrls,
      activeTab: vm.activeTab,
      activeSubpage: vm.activeSubpage,
      pageTitle: vm.pageTitle,
      pageUrl: vm.pageUrl,
      sidebarCollapsed: vm.sidebarCollapsed,
    };
  });
  if (expected.openedPrefix) {
    assert.ok(result.opened.startsWith(expected.openedPrefix), `${result.opened} should start with ${expected.openedPrefix}`);
  } else {
    assert.strictEqual(result.opened, '', 'desktop mode should not open a separate terminal window');
  }
  const targetUrl = expected.openedPrefix ? result.opened : result.pageUrl;
  assert.ok(targetUrl.startsWith(expected.urlPrefix), `${targetUrl} should start with ${expected.urlPrefix}`);
  assert.ok(targetUrl.includes('token=token%20with%20spaces'), `${targetUrl} should URL-encode the token`);
  if (expected.localTerminal) {
    assert.ok(targetUrl.includes('startupServerId=2'), `${targetUrl} should include startupServerId`);
    assert.ok(targetUrl.includes('startupTitle=prod%20shell'), `${targetUrl} should include startupTitle`);
    assert.ok(targetUrl.includes('startupNonce='), `${targetUrl} should include a startup nonce so the iframe reloads for repeated launches`);
  } else {
    assert.ok(targetUrl.includes('serverId=2'), `${targetUrl} should include serverId`);
    assert.ok(targetUrl.includes('osType=linux'), `${targetUrl} should include osType`);
  }
  if (expected.embedded) {
    assert.strictEqual(result.activeTab, 'page', 'desktop terminal should render inside the main workspace');
    assert.strictEqual(result.activeSubpage, 'remote-terminal');
    assert.strictEqual(result.pageTitle, '服务器终端');
    assert.strictEqual(result.sidebarCollapsed, true, 'desktop terminal should collapse the sidebar for more terminal space');
  } else {
    assert.strictEqual(result.sidebarCollapsed, false, 'web terminal launch should not change sidebar state');
  }
}

async function assertRepeatedDesktopLaunchesReload(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-repeat.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    window.open = function() {};
    const vm = app();
    await vm.openTerminalFull({ id: 2, name: 'prod shell', ip: '10.0.0.8', os_type: 'linux' });
    const first = vm.pageUrl;
    await new Promise((resolve) => setTimeout(resolve, 2));
    await vm.openTerminalFull({ id: 2, name: 'prod shell', ip: '10.0.0.8', os_type: 'linux' });
    return { first, second: vm.pageUrl };
  });
  assert.strictEqual(result.first, result.second, 'reopening while the desktop terminal iframe is already mounted should keep the iframe and request a fresh tab via postMessage');
}

async function assertDesktopLaunchPostsToExistingTerminal(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-post.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    const posted = [];
    const iframe = document.getElementById('local-terminal-frame');
    Object.defineProperty(iframe, 'contentWindow', {
      value: { postMessage: (message, targetOrigin) => posted.push({ message, targetOrigin }) },
      configurable: true,
    });
    const vm = app();
    vm.activeTab = 'page';
    vm.activeSubpage = 'remote-terminal';
    vm.pageUrl = 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor';
    await vm.openTerminalFull({ id: 3, name: 'mysql', ip: '10.0.0.8', os_type: 'linux' });
    return { posted, pageUrl: vm.pageUrl, sidebarCollapsed: vm.sidebarCollapsed };
  });
  assert.strictEqual(result.posted.length, 1, 'existing desktop local terminal iframe should receive a message instead of being reused silently');
  assert.deepStrictEqual(result.posted[0].message, {
    type: 'open-server-terminal',
    serverId: 3,
    title: 'mysql',
  });
  assert.strictEqual(result.posted[0].targetOrigin, '*');
  assert.ok(result.pageUrl.includes('startupServerId=1'), 'postMessage launch should keep the existing local terminal iframe and let it add a new tab');
  assert.strictEqual(result.sidebarCollapsed, true);
}

async function assertDesktopLocalTerminalMenuUsesDesktopUrl(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token with spaces');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-local-terminal.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    const vm = app();
    vm.openLocalTerminal();
    return { activeSubpage: vm.activeSubpage, pageUrl: vm.pageUrl, pageTitle: vm.pageTitle };
  });
  assert.strictEqual(result.activeSubpage, 'local-terminal');
  assert.strictEqual(result.pageTitle, '本地终端');
  assert.ok(result.pageUrl.startsWith('http://127.0.0.1:45678/local-terminal?'), `${result.pageUrl} should use the desktop HTTP server`);
  assert.ok(result.pageUrl.includes('token=token%20with%20spaces'), `${result.pageUrl} should URL-encode token`);
}

async function assertDesktopWebTerminalLaunch(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token with spaces');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-web-terminal.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    window.__openedUrls = [];
    window.open = function(url) { window.__openedUrls.push(url); };
    const vm = app();
    vm.openWebTerminal({ id: 2, name: 'prod shell', os_type: 'linux' });
    return {
      opened: window.__openedUrls[0] || '',
      activeTab: vm.activeTab,
      activeSubpage: vm.activeSubpage,
      pageUrl: vm.pageUrl,
    };
  });
  assert.strictEqual(result.opened, '', 'desktop web terminal should stay inside the main window');
  assert.ok(result.pageUrl.startsWith('http://127.0.0.1:45678/terminal?'), `${result.pageUrl} should use the desktop HTTP server`);
  assert.ok(result.pageUrl.includes('token=token%20with%20spaces'), `${result.pageUrl} should URL-encode token`);
  assert.ok(result.pageUrl.includes('serverId=2'), `${result.pageUrl} should include serverId`);
  assert.ok(result.pageUrl.includes('osType=linux'), `${result.pageUrl} should include osType`);
  assert.strictEqual(result.activeTab, 'page');
  assert.strictEqual(result.activeSubpage, 'web-terminal');
}

async function assertDesktopWebTerminalSurvivesOtherPages(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-web-terminal-persist.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    const vm = app();
    vm.openWebTerminal({ id: 2, name: 'prod shell', os_type: 'linux' });
    const firstWeb = vm.webTerminalUrl;
    vm.loadPage('tunnels', '/tunnels?token=token');
    const afterTunnels = {
      pageUrl: vm.pageUrl,
      webTerminalUrl: vm.webTerminalUrl,
      activeSubpage: vm.activeSubpage,
      webFrameSrc: document.getElementById('web-terminal-frame').getAttribute(':src'),
      pageFrameSrc: document.getElementById('page-frame').getAttribute(':src'),
    };
    vm.openWebTerminal({ id: 2, name: 'prod shell', os_type: 'linux' });
    return {
      firstWeb,
      afterTunnels,
      finalPageUrl: vm.pageUrl,
      finalWebTerminalUrl: vm.webTerminalUrl,
      finalSubpage: vm.activeSubpage,
      finalTitle: vm.pageTitle,
    };
  });
  assert.ok(result.firstWeb.includes('/terminal?token=token'), 'initial web terminal URL should be created');
  assert.strictEqual(result.afterTunnels.pageUrl, '/tunnels?token=token', 'ordinary pages should use the regular page URL');
  assert.strictEqual(result.afterTunnels.webTerminalUrl, result.firstWeb, 'ordinary pages should not clear the mounted web terminal URL');
  assert.strictEqual(result.finalPageUrl, result.firstWeb, 'returning to the same web terminal should reuse the preserved URL');
  assert.strictEqual(result.finalWebTerminalUrl, result.firstWeb, 'web terminal iframe should stay mounted across ordinary navigation');
  assert.strictEqual(result.finalSubpage, 'web-terminal');
  assert.strictEqual(result.finalTitle, 'Web终端');
}

async function assertDesktopWebTerminalMenuReturn(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-web-terminal-menu.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    window.__alerts = [];
    window.alert = function(msg) { window.__alerts.push(msg); };
    const vm = app();
    vm.returnWebTerminal();
    const firstAlert = window.__alerts[0] || '';
    vm.openWebTerminal({ id: 2, name: 'prod shell', os_type: 'linux' });
    const openedUrl = vm.webTerminalUrl;
    vm.loadPage('files', '/files?token=token');
    vm.returnWebTerminal();
    return {
      firstAlert,
      openedUrl,
      pageUrl: vm.pageUrl,
      activeTab: vm.activeTab,
      activeSubpage: vm.activeSubpage,
      pageTitle: vm.pageTitle,
      sidebarCollapsed: vm.sidebarCollapsed,
    };
  });
  assert.ok(result.firstAlert.includes('还没有打开过 Web 终端'), 'web terminal menu should explain how to open the first web terminal');
  assert.strictEqual(result.pageUrl, result.openedUrl, 'web terminal menu should return to the preserved web terminal URL');
  assert.strictEqual(result.activeTab, 'page');
  assert.strictEqual(result.activeSubpage, 'web-terminal');
  assert.strictEqual(result.pageTitle, 'Web终端');
  assert.strictEqual(result.sidebarCollapsed, true);
}

async function assertDesktopSettingsMenu(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
    window.go = { main: { App: {
      GetDesktopSettings: () => Promise.resolve({ fixedPort: 34567, allowLAN: true, currentPort: 45678, currentBindHost: '0.0.0.0', settingsPath: 'C:/app/desktop-settings.json', restartRequired: false }),
      SaveDesktopSettings: (settings) => Promise.resolve(Object.assign({}, settings, { currentPort: 45678, currentBindHost: '0.0.0.0', settingsPath: 'C:/app/desktop-settings.json', restartRequired: true })),
    } } };
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-settings.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    window.alert = function(msg) { window.__lastAlert = msg; };
    const vm = app();
    await vm.openSettings();
    vm.desktopSettings.fixedPort = 45679;
    vm.desktopSettings.allowLAN = false;
    await vm.saveDesktopSettings();
    return {
      activeTab: vm.activeTab,
      pageTitle: vm.pageTitle,
      settings: vm.desktopSettings,
      alert: window.__lastAlert,
    };
  });
  assert.strictEqual(result.activeTab, 'settings');
  assert.strictEqual(result.pageTitle, '设置');
  assert.strictEqual(result.settings.fixedPort, 45679);
  assert.strictEqual(result.settings.allowLAN, false);
  assert.strictEqual(result.settings.restartRequired, true);
  assert.ok(result.alert.includes('重启'), 'saving changed desktop connection settings should tell the user to restart');
}

async function assertServerCardHasTwoTerminalButtons(page, html) {
  assert.ok(html.includes('Web终端'), 'server cards should expose a Web terminal action');
  assert.ok(html.includes('openWebTerminal(server)'), 'Web terminal button should call openWebTerminal');
  assert.ok(html.includes('本地终端'), 'server cards should expose a local terminal action');
  assert.ok(html.includes('openTerminalFull(server)'), 'local terminal button should call openTerminalFull');
}

async function assertDesktopLocalTerminalMenuKeepsExistingTerminal(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-local-terminal-reuse.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    const vm = app();
    vm.activeTab = 'page';
    vm.activeSubpage = 'remote-terminal';
    vm.pageTitle = '服务器终端';
    vm.pageUrl = 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor';
    vm.openLocalTerminal();
    return { activeSubpage: vm.activeSubpage, pageTitle: vm.pageTitle, pageUrl: vm.pageUrl };
  });
  assert.strictEqual(result.pageUrl, 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor', 'local terminal menu should not reload an existing local terminal iframe');
  assert.strictEqual(result.activeSubpage, 'remote-terminal', 'local terminal menu should be a no-op when a server terminal already uses the local terminal page');
  assert.strictEqual(result.pageTitle, '服务器终端');
}

async function assertDesktopLocalTerminalSurvivesOtherPages(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-local-terminal-persist.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(() => {
    const vm = app();
    vm.openLocalTerminal();
    const firstLocal = vm.localTerminalUrl;
    vm.loadPage('tunnels', '/tunnels?token=token');
    const afterTunnels = {
      pageUrl: vm.pageUrl,
      localTerminalUrl: vm.localTerminalUrl,
      activeSubpage: vm.activeSubpage,
      localFrameSrc: document.getElementById('local-terminal-frame').getAttribute(':src'),
      pageFrameSrc: document.getElementById('page-frame').getAttribute(':src'),
    };
    vm.loadPage('terminal-logs', '/terminal-logs?token=token');
    vm.openLocalTerminal();
    return {
      firstLocal,
      afterTunnels,
      finalPageUrl: vm.pageUrl,
      finalLocalTerminalUrl: vm.localTerminalUrl,
      finalSubpage: vm.activeSubpage,
      finalTitle: vm.pageTitle,
    };
  });
  assert.ok(result.firstLocal.includes('/local-terminal?token=token'), 'initial local terminal URL should be created');
  assert.strictEqual(result.afterTunnels.pageUrl, '/tunnels?token=token', 'ordinary pages should use the regular page URL');
  assert.strictEqual(result.afterTunnels.localTerminalUrl, result.firstLocal, 'ordinary pages should not clear or replace the mounted local terminal URL');
  assert.strictEqual(result.finalPageUrl, result.firstLocal, 'returning to local terminal should reuse the original URL instead of refreshing');
  assert.strictEqual(result.finalLocalTerminalUrl, result.firstLocal, 'local terminal iframe should stay mounted across ordinary page navigation');
  assert.strictEqual(result.finalSubpage, 'local-terminal');
  assert.strictEqual(result.finalTitle, '本地终端');
}

async function assertServerLaunchAfterLocalMenuStillPosts(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-local-menu-then-server.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    const posted = [];
    const iframe = document.getElementById('local-terminal-frame');
    Object.defineProperty(iframe, 'contentWindow', {
      value: { postMessage: (message, targetOrigin) => posted.push({ message, targetOrigin }) },
      configurable: true,
    });
    const vm = app();
    vm.activeTab = 'page';
    vm.activeSubpage = 'remote-terminal';
    vm.pageTitle = '服务器终端';
    vm.pageUrl = 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor';
    vm.openLocalTerminal();
    const afterMenu = { activeSubpage: vm.activeSubpage, pageTitle: vm.pageTitle, pageUrl: vm.pageUrl };
    await vm.openTerminalFull({ id: 2, name: 'mysql', ip: '10.0.0.8', os_type: 'linux' });
    return { posted, afterMenu, finalPageUrl: vm.pageUrl };
  });
  assert.deepStrictEqual(result.afterMenu, {
    activeSubpage: 'remote-terminal',
    pageTitle: '服务器终端',
    pageUrl: 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor',
  });
  assert.strictEqual(result.posted.length, 1, 'opening another server after clicking local terminal should still append a tab via postMessage');
  assert.deepStrictEqual(result.posted[0].message, { type: 'open-server-terminal', serverId: 2, title: 'mysql' });
  assert.strictEqual(result.finalPageUrl, result.afterMenu.pageUrl, 'opening another server should not replace the existing local terminal iframe');
}

async function assertServerLaunchFromServerListStillPosts(page, html) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'token');
    localStorage.setItem('user', JSON.stringify({ username: 'admin' }));
  });
  await page.route('**/*', (route) => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  await page.goto('http://desktop-index-server-list-then-terminal.test/', { waitUntil: 'domcontentloaded' });
  const result = await page.evaluate(async () => {
    const posted = [];
    const iframe = document.getElementById('local-terminal-frame');
    Object.defineProperty(iframe, 'contentWindow', {
      value: { postMessage: (message, targetOrigin) => posted.push({ message, targetOrigin }) },
      configurable: true,
    });
    const vm = app();
    vm.activeTab = 'servers';
    vm.activeSubpage = 'remote-terminal';
    vm.pageTitle = '服务器终端';
    vm.pageUrl = 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor';
    await vm.openTerminalFull({ id: 2, name: 'mysql', ip: '10.0.0.8', os_type: 'linux' });
    return {
      posted,
      activeTab: vm.activeTab,
      activeSubpage: vm.activeSubpage,
      pageTitle: vm.pageTitle,
      pageUrl: vm.pageUrl,
      sidebarCollapsed: vm.sidebarCollapsed,
    };
  });
  assert.strictEqual(result.posted.length, 1, 'opening a server from the server list should append a tab in the existing local terminal iframe');
  assert.deepStrictEqual(result.posted[0].message, { type: 'open-server-terminal', serverId: 2, title: 'mysql' });
  assert.strictEqual(result.activeTab, 'page', 'opening a server should switch back to the terminal page');
  assert.strictEqual(result.activeSubpage, 'remote-terminal');
  assert.strictEqual(result.pageTitle, '服务器终端');
  assert.strictEqual(result.pageUrl, 'http://127.0.0.1:45678/local-terminal?token=token&startupServerId=1&startupTitle=harbor', 'existing terminal iframe URL should not be replaced');
  assert.strictEqual(result.sidebarCollapsed, true);
}

async function main() {
  const executablePath = findBrowserExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  try {
    let page = await browser.newPage();
    await assertTerminalLaunch(page, renderTemplate({ desktop: true, port: 45678 }), {
      embedded: true,
      localTerminal: true,
      urlPrefix: 'http://127.0.0.1:45678/local-terminal?',
    });
    await page.close();

    page = await browser.newPage();
    await assertRepeatedDesktopLaunchesReload(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopLaunchPostsToExistingTerminal(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopLocalTerminalMenuUsesDesktopUrl(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopWebTerminalLaunch(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopWebTerminalSurvivesOtherPages(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopWebTerminalMenuReturn(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopSettingsMenu(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertServerCardHasTwoTerminalButtons(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopLocalTerminalMenuKeepsExistingTerminal(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertDesktopLocalTerminalSurvivesOtherPages(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertServerLaunchAfterLocalMenuStillPosts(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertServerLaunchFromServerListStillPosts(page, renderTemplate({ desktop: true, port: 45678 }));
    await page.close();

    page = await browser.newPage();
    await assertTerminalLaunch(page, renderTemplate({ desktop: false, port: 0 }), {
      openedPrefix: '/terminal?',
      urlPrefix: '/terminal?',
    });
    await page.close();
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
