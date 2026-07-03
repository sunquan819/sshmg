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
window.__clipboardWrites = [];
window.__sentData = [];
window.__postedBodies = [];
window.__fitCols = 94;
window.__fitRows = 27;
window.__scrollLines = [];
window.__webSockets = [];
window.__sentBySocket = [];
window.__terminals = [];
window.navigator.clipboard = {
  writeText: function(text) { window.__clipboardWrites.push(text); return Promise.resolve(); },
  readText: function() { return Promise.resolve('pasted-text'); },
};
window.fetch = function(url, opts) {
  url = String(url);
  if (url.indexOf('/api/local-terminal/agents/opencode/sessions') >= 0) {
    return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [{ id: 'ses_123', title: 'Fix deploy', projectPath: 'C:/repo' }] }); } });
  }
  if (url.indexOf('/api/local-terminal/agents') >= 0) {
    return Promise.resolve({ json: function() { return Promise.resolve({ agents: [{ id: 'opencode', name: 'opencode', installed: true, supportsHistory: true, supportsLatest: true, path: 'C:/opencode.ps1' }] }); } });
  }
  if (opts && opts.method === 'POST') {
    window.__postedBodies.push(opts.body ? JSON.parse(opts.body) : {});
    return Promise.resolve({ json: function() { return Promise.resolve({ id: 'session-' + (++window.__sid) }); } });
  }
  return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [] }); } });
};
window.WebSocket = function() {
  this.__socketId = window.__webSockets.length;
  window.__webSockets.push(this);
  this.readyState = 1;
  setTimeout(() => this.onopen && this.onopen(), 0);
};
window.WebSocket.prototype.send = function() {};
window.WebSocket.prototype.send = function(data) {
  window.__sentData.push(data);
  window.__sentBySocket.push({ socketId: this.__socketId, data });
};
window.WebSocket.prototype.close = function() {
  this.readyState = 3;
  if (this.onclose) this.onclose();
};
window.Terminal = function() {
  window.__terminalOptions = window.__terminalOptions || [];
  window.__terminalOptions.push(arguments[0] || {});
  this.cols = 80;
  this.rows = 24;
  this.options = {};
  this.buffer = { active: { cursorX: 0, cursorY: 0, length: 0, getLine: function() { return null; } } };
  this.parser = { registerOscHandler: function() {} };
  this.__selection = 'selected-text';
  this.__selectionPosition = { start: { x: 2, y: 3 }, end: { x: 8, y: 3 } };
  this.__terminalId = window.__terminals.length;
  window.__terminals.push(this);
  window.__lastTerminal = this;
};
window.Terminal.prototype.loadAddon = function() {};
window.Terminal.prototype.open = function(el) {
  var term = document.createElement('div');
  term.className = 'xterm';
  term.addEventListener('mousedown', function(e) {
    window.__xtermMouseDownShiftStates = window.__xtermMouseDownShiftStates || [];
    window.__xtermMouseDownShiftStates.push(e.shiftKey);
  });
  term.addEventListener('mouseup', function() {
    window.__xtermMouseUpCount = (window.__xtermMouseUpCount || 0) + 1;
  });
  el.appendChild(term);
  this.element = term;
};
window.Terminal.prototype.focus = function() {
  window.__focusCount = (window.__focusCount || 0) + 1;
};
window.Terminal.prototype.dispose = function() {};
window.Terminal.prototype.writeln = function() {};
window.Terminal.prototype.write = function() {};
window.Terminal.prototype.onData = function(handler) { this.__onData = handler; };
window.Terminal.prototype.onResize = function(handler) { this.__resizeHandler = handler; };
window.Terminal.prototype.scrollLines = function(lines) { window.__scrollLines.push(lines); };
window.Terminal.prototype.attachCustomKeyEventHandler = function(handler) { this.__keyHandler = handler; };
window.Terminal.prototype.getSelection = function() { return this.__selection || ''; };
window.Terminal.prototype.getSelectionPosition = function() { return this.__selectionPosition; };
window.Terminal.prototype.select = function(x, y, size) {
  this.__selection = 'restored-selection';
  this.__selectedRange = { x, y, size };
};
window.Terminal.prototype.clearSelection = function() {
  this.__selection = '';
  window.__clearSelectionCount = (window.__clearSelectionCount || 0) + 1;
};
window.FitAddon = { FitAddon: function() { this.fit = function() {
  if (!window.__lastTerminal) return;
  window.__lastTerminal.cols = window.__fitCols;
  window.__lastTerminal.rows = window.__fitRows;
  if (window.__lastTerminal.__resizeHandler) window.__lastTerminal.__resizeHandler({ cols: window.__fitCols, rows: window.__fitRows });
}; } };
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
    await page.goto('http://local-terminal.test/', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 1, null, { timeout: 5000 })
      .catch((err) => {
        if (pageErrors.length) {
          throw new Error(pageErrors.join('\n'));
        }
        throw err;
      });

    const terminalChrome = await page.evaluate(() => {
      const pane = document.querySelector('.pane-item');
      const xterm = document.querySelector('.pane-item .xterm');
      return {
        panePadding: getComputedStyle(pane).padding,
        xtermPadding: getComputedStyle(xterm).padding,
        windowsMode: window.__terminalOptions[0].windowsMode,
      };
    });
    assert.strictEqual(terminalChrome.xtermPadding, '0px', 'xterm root must not have padding because it offsets mouse selection coordinates');
    assert.match(terminalChrome.panePadding, /^2px/, 'pane spacing should live outside the xterm coordinate surface');
    assert.strictEqual(terminalChrome.windowsMode, true, 'terminal should use xterm windowsMode for Windows Terminal-like mouse behavior');
    await page.waitForFunction(() => window.__sentData.some((data) => {
      try {
        const message = JSON.parse(data);
        return message.type === 'resize' && message.cols === 94 && message.rows === 27;
      } catch (err) {
        return false;
      }
    }), null, { timeout: 5000 });
    await page.evaluate(() => {
      window.__sentData = [];
      window.__fitCols = 132;
      window.__fitRows = 38;
      window.dispatchEvent(new Event('resize'));
    });
    await page.waitForFunction(() => window.__sentData.some((data) => {
      try {
        const message = JSON.parse(data);
        return message.type === 'resize' && message.cols === 132 && message.rows === 38;
      } catch (err) {
        return false;
      }
    }), null, { timeout: 5000 });

    await page.evaluate(() => {
      window.__scrollLines = [];
      window.__lastTerminal.modes = { mouseTrackingMode: 'none' };
      document.querySelector('.pane-item').dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 120 }));
    });
    assert.deepStrictEqual(await page.evaluate(() => window.__scrollLines), [3], 'mouse wheel should scroll terminal history in normal mode');

    await page.evaluate(() => {
      window.__scrollLines = [];
      window.__lastTerminal.modes = { mouseTrackingMode: 'any' };
      document.querySelector('.pane-item').dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 120 }));
    });
    assert.deepStrictEqual(await page.evaluate(() => window.__scrollLines), [], 'mouse wheel should be left to TUI apps while mouse tracking is active');

    await page.evaluate(() => {
      window.__focusCount = 0;
      window.__xtermMouseDownShiftStates = [];
      document.querySelector('.pane-item').dispatchEvent(new MouseEvent('mousedown', { bubbles: true, shiftKey: true, button: 0 }));
    });
    assert.strictEqual(await page.evaluate(() => window.__focusCount), 1, 'Shift+left mouse selection should still focus the terminal while bubbling to xterm');

    await page.evaluate(() => {
      window.__lastTerminal.modes = { mouseTrackingMode: 'none' };
      window.__xtermMouseDownShiftStates = [];
      window.__lastTerminal.__selection = 'previous-selection';
      const xterm = document.querySelector('.pane-item .xterm');
      xterm.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true, shiftKey: true, button: 0 }));
    });
    assert.deepStrictEqual(
      await page.evaluate(() => window.__xtermMouseDownShiftStates),
      [false],
      'Shift+left in normal mode should start a fresh selection instead of extending the old one'
    );

    await page.evaluate(() => {
      window.__lastTerminal.modes = { mouseTrackingMode: 'any' };
      window.__xtermMouseDownShiftStates = [];
      window.__lastTerminal.__selection = 'previous-selection';
      const xterm = document.querySelector('.pane-item .xterm');
      xterm.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true, shiftKey: true, button: 0 }));
    });
    assert.deepStrictEqual(
      await page.evaluate(() => window.__xtermMouseDownShiftStates),
      [true],
      'Shift+left in mouse tracking mode should keep Shift so xterm can force selection'
    );
    await page.evaluate(() => {
      window.__xtermMouseUpCount = 0;
      window.__documentMouseUpCount = 0;
      window.__lastTerminal.__selection = 'forced-selection';
      window.__lastTerminal.__selectionPosition = { start: { x: 2, y: 3 }, end: { x: 8, y: 3 } };
      document.addEventListener('mouseup', function handler(e) {
        if (e.__dmShiftSelectionSynthetic) window.__documentMouseUpCount += 1;
        document.removeEventListener('mouseup', handler);
      });
      const xterm = document.querySelector('.pane-item .xterm');
      xterm.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, cancelable: true, shiftKey: true, button: 0 }));
      window.__lastTerminal.clearSelection();
    });
    await page.waitForTimeout(120);
    assert.strictEqual(await page.evaluate(() => window.__xtermMouseUpCount), 0, 'forced selection mouseup should not be sent to the terminal app mouse handler');
    assert.strictEqual(await page.evaluate(() => window.__documentMouseUpCount), 1, 'forced selection mouseup should still reach xterm selection finalization on document');
    assert.deepStrictEqual(
      await page.evaluate(() => window.__lastTerminal.__selectedRange),
      { x: 2, y: 3, size: 6 },
      'forced selection should be restored if xterm clears it after mouseup'
    );

    await page.evaluate(() => {
      window.__clipboardWrites = [];
      document.querySelector('.pane-item').dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    });
    await page.waitForTimeout(50);
    const mouseupClipboardWrites = await page.evaluate(() => window.__clipboardWrites.length);
    assert.strictEqual(mouseupClipboardWrites, 0, 'mouse selection should not copy automatically on mouseup');

    const copyShortcutResult = await page.evaluate(() => {
      window.__clipboardWrites = [];
      window.__clearSelectionCount = 0;
      window.__lastTerminal.__selection = 'selected-text';
      return window.__lastTerminal.__keyHandler({
        type: 'keydown',
        ctrlKey: true,
        shiftKey: true,
        key: 'C',
        code: 'KeyC',
        preventDefault: function() { this.prevented = true; },
      });
    });
    await page.waitForTimeout(50);
    assert.strictEqual(copyShortcutResult, false, 'Ctrl+Shift+C should be handled by the terminal page');
    assert.deepStrictEqual(await page.evaluate(() => window.__clipboardWrites), ['selected-text']);
    assert.strictEqual(await page.evaluate(() => window.__clearSelectionCount), 1, 'Ctrl+Shift+C should clear the selection after copying');

    await page.evaluate(() => {
      window.__clipboardWrites = [];
      window.__sentData = [];
      window.__clearSelectionCount = 0;
      window.__lastTerminal.__selection = 'right-click-selection';
      document.querySelector('.pane-item').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(50);
    assert.deepStrictEqual(await page.evaluate(() => window.__clipboardWrites), ['right-click-selection'], 'right-click with a selection should copy it');
    assert.deepStrictEqual(await page.evaluate(() => window.__sentData), [], 'right-click with a selection should not paste');
    assert.strictEqual(await page.evaluate(() => window.__clearSelectionCount), 1, 'right-click copy should clear the selection');

    await page.evaluate(() => {
      window.__clipboardWrites = [];
      window.__sentData = [];
      window.__lastTerminal.__selection = '';
      document.querySelector('.pane-item').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(50);
    assert.deepStrictEqual(await page.evaluate(() => window.__clipboardWrites), [], 'right-click without a selection should not copy');
    assert.deepStrictEqual(await page.evaluate(() => window.__sentData), ['pasted-text'], 'right-click without a selection should paste');

    await page.evaluate(() => {
      window.__sentData = [];
      window.__lastTerminal.__onData('\\x03');
    });
    assert.deepStrictEqual(await page.evaluate(() => window.__sentData), ['\\x03'], 'Ctrl+C should still send interrupt to the shell');
    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 2);
    await page.evaluate(() => {
      window.__sentBySocket = [];
      window.__terminals[0].__selection = 'echo from first pane';
      document.querySelectorAll('.pane-item')[0].dispatchEvent(new MouseEvent('mouseup', { bubbles: true, clientX: 120, clientY: 120 }));
    });
    await page.waitForFunction(() => {
      const menu = document.getElementById('send-selection-menu');
      return menu && menu.style.display === 'block' && menu.textContent.indexOf('当前分屏') >= 0;
    });
    await page.evaluate(() => {
      document.querySelector('#send-selection-menu .send-selection-primary').click();
    });
    assert.deepStrictEqual(
      await page.evaluate(() => window.__sentBySocket.filter((entry) => entry.data === 'echo from first pane')),
      [{ socketId: 1, data: 'echo from first pane' }],
      'selected text should be sent to the other split pane, not back to the source pane'
    );

    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 3);
    await page.evaluate(() => {
      window.__sentBySocket = [];
      window.__terminals[0].__selection = 'fan out to split panes';
      document.querySelectorAll('.pane-item')[0].dispatchEvent(new MouseEvent('mouseup', { bubbles: true, clientX: 140, clientY: 140 }));
    });
    await page.waitForFunction(() => {
      const menu = document.getElementById('send-selection-menu');
      return menu && menu.style.display === 'block' && menu.textContent.indexOf('当前分屏') >= 0;
    });
    await page.evaluate(() => {
      document.querySelector('#send-selection-menu .send-selection-primary').click();
    });
    assert.deepStrictEqual(
      await page.evaluate(() => window.__sentBySocket.filter((entry) => entry.data === 'fan out to split panes')),
      [
        { socketId: 1, data: 'fan out to split panes' },
        { socketId: 2, data: 'fan out to split panes' },
      ],
      'selected text should fan out to every other pane in the current split group'
    );
    await page.waitForFunction(() => {
      const activeTab = document.querySelector('#tab-bar .tab.active');
      return activeTab && activeTab.textContent.indexOf('已发送') >= 0;
    });

    await page.evaluate(() => {
      window.__sentBySocket = [];
      window.__terminals[0].__selection = 'send to self pane';
      document.querySelectorAll('.pane-item')[0].dispatchEvent(new MouseEvent('mouseup', { bubbles: true, clientX: 140, clientY: 140 }));
    });
    await page.waitForFunction(() => {
      const menu = document.getElementById('send-selection-menu');
      const item = document.querySelector('#send-selection-menu .send-selection-self');
      return menu && menu.style.display === 'block' && item && item.textContent.indexOf('当前终端') >= 0;
    });
    await page.evaluate(() => {
      document.querySelector('#send-selection-menu .send-selection-self').click();
    });
    assert.deepStrictEqual(
      await page.evaluate(() => window.__sentBySocket.filter((entry) => entry.data === 'send to self pane')),
      [{ socketId: 0, data: 'send to self pane' }],
      'selected text should be sendable back to the source terminal'
    );

    await page.evaluate(() => {
      window.__clipboardWrites = [];
      window.__terminals[0].__selection = 'copy closes send menu';
      document.querySelectorAll('.pane-item')[0].dispatchEvent(new MouseEvent('mouseup', { bubbles: true, clientX: 120, clientY: 120 }));
    });
    await page.waitForFunction(() => document.getElementById('send-selection-menu').style.display === 'block');
    await page.evaluate(() => {
      document.querySelectorAll('.pane-item')[0].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(50);
    assert.deepStrictEqual(await page.evaluate(() => window.__clipboardWrites), ['copy closes send menu'], 'right-click should still copy the selected terminal text');
    assert.strictEqual(
      await page.evaluate(() => document.getElementById('send-selection-menu').style.display),
      'none',
      'right-click copy should close the send-to-terminal menu'
    );
    await page.evaluate(() => document.querySelectorAll('.pane-close')[2].click());
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 2);
    await page.evaluate(() => document.querySelectorAll('.pane-close')[1].click());
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 1);

    const layout = await page.evaluate(() => {
      const container = document.querySelector('.pane-container.active');
      const children = Array.from(container.children);
      return {
        childClasses: children.map((child) => child.className || child.tagName),
        directPaneCount: children.filter((child) => child.classList.contains('pane-item')).length,
        dividerCount: container.querySelectorAll('.pane-divider').length,
        staleSlotCount: children.filter((child) =>
          !child.classList.contains('pane-item') && !child.classList.contains('pane-divider')
        ).length,
      };
    });

    assert.strictEqual(layout.directPaneCount, 1, 'remaining pane should be a direct child of the tab container');
    assert.strictEqual(layout.dividerCount, 0, 'closing back to one pane should remove dividers');
    assert.strictEqual(layout.staleSlotCount, 0, 'closing a split pane should not leave empty wrapper slots');

    await page.evaluate(() => window.splitPane('horizontal'));
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 2);
    const horizontalLayout = await page.evaluate(() => {
      const container = document.querySelector('.pane-container.active');
      return {
        flexDirection: getComputedStyle(container).flexDirection,
        verticalDividerCount: container.querySelectorAll('.pane-divider-v').length,
      };
    });
    assert.strictEqual(horizontalLayout.flexDirection, 'column', 'horizontal split should stack panes top to bottom');
    assert.strictEqual(horizontalLayout.verticalDividerCount, 1, 'horizontal split should use row-resize dividers');

    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.pane-container.active')).flexDirection === 'row');
    const verticalLayout = await page.evaluate(() => {
      const container = document.querySelector('.pane-container.active');
      return {
        flexDirection: getComputedStyle(container).flexDirection,
        horizontalDividerCount: container.querySelectorAll('.pane-divider-h').length,
      };
    });
    assert.strictEqual(verticalLayout.flexDirection, 'row', 'vertical split should place panes side by side');
    assert.strictEqual(verticalLayout.horizontalDividerCount, 2, 'three vertical panes should have two column-resize dividers');

    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForFunction(() => document.querySelectorAll('.pane-item').length === 4);
    await page.evaluate(() => window.splitPane('vertical'));
    await page.waitForTimeout(100);
    const cappedLayout = await page.evaluate(() => ({
      paneCount: document.querySelectorAll('.pane-item').length,
      statusText: document.getElementById('st-conn').textContent,
    }));
    assert.strictEqual(cappedLayout.paneCount, 4, 'split panes should be capped at four');
    assert.match(cappedLayout.statusText, /最多 4 个分屏/, 'cap should be visible in the status bar');

    await page.waitForFunction(() => document.querySelector('#agent-btn'));
    await page.click('#agent-btn');
    await page.waitForFunction(() => document.querySelector('#agent-menu').style.display === 'block');
    assert.ok(await page.evaluate(() => document.querySelector('#agent-menu').textContent.includes('opencode')));
    await page.evaluate(() => { window.__postedBodies = []; });
    await page.click('[data-agent-action="opencode:new"]');
    await page.waitForTimeout(50);
    assert.deepStrictEqual(await page.evaluate(() => window.__postedBodies.pop()), { agentId: 'opencode', mode: 'new', sessionId: '' });
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
