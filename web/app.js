// The Single Source of Truth
const state = {
    // Open tabs, in tab-bar order. Each tab:
    // { id, path (null while untitled), name, editor (<textarea>), el (tab DOM),
    //   savedContent, version, lineEnding }
    tabs: [],
    activeTab: null,
    nextTabId: 1,
    // Last clicked folder, or the folder of the open file. Prefills "+ File".
    selectedPath: ".",
    selectedIsDir: true
};

function selectedDir() {
    if (state.selectedIsDir) return state.selectedPath;
    const i = state.selectedPath.lastIndexOf('/');
    return i === -1 ? '.' : state.selectedPath.slice(0, i);
}

// Selected folder as a path prefix for dialogs ("" at the workspace root)
function selectedDirPrefix() {
    return selectedDir() === '.' ? '' : selectedDir() + '/';
}

function select(path, isDir) {
    state.selectedPath = path;
    state.selectedIsDir = isDir;
    highlightSelection();
}

function highlightSelection() {
    DOM.tree.querySelectorAll('.tree-node').forEach(el => {
        el.classList.toggle('selected', el.dataset.path === state.selectedPath);
    });
}

const DOM = {
    tree: document.getElementById('tree'),
    tabs: document.getElementById('tabs'),
    tabbar: document.getElementById('tabbar'),
    editors: document.getElementById('editors'),
    statPath: document.getElementById('stat-path'),
    statDirty: document.getElementById('stat-dirty'),
    statCounts: document.getElementById('stat-counts'),
    dialog: document.getElementById('dialog'),
    dialogTitle: document.getElementById('dialog-title'),
    dialogMessage: document.getElementById('dialog-message'),
    dialogInput: document.getElementById('dialog-input'),
    dialogOk: document.getElementById('dialog-ok'),
    dialogCancel: document.getElementById('dialog-cancel')
};

// In-page replacement for prompt/alert/confirm.
// With `input`, resolves to the trimmed text (or null if cancelled);
// otherwise resolves to true (OK) / false (Cancel or Esc).
// `selection` is an optional [start, end] range to pre-select in the input.
function showDialog({ title, message = '', input = false, value = '', selection = null, placeholder = '', okLabel = 'OK', cancel = true, danger = false }) {
    return new Promise(resolve => {
        DOM.dialogTitle.textContent = title;
        DOM.dialogMessage.textContent = message;
        DOM.dialogInput.hidden = !input;
        DOM.dialogInput.value = value;
        DOM.dialogInput.placeholder = placeholder;
        DOM.dialogOk.textContent = okLabel;
        DOM.dialogOk.classList.toggle('danger', danger);
        DOM.dialogCancel.hidden = !cancel;
        DOM.dialog.returnValue = '';

        DOM.dialog.addEventListener('close', () => {
            const ok = DOM.dialog.returnValue === 'ok';
            resolve(input ? (ok ? DOM.dialogInput.value.trim() || null : null) : ok);
        }, { once: true });

        DOM.dialog.showModal();
        if (input) {
            DOM.dialogInput.focus();
            // Default: cursor at the end so a prefilled folder path can be typed after
            const [start, end] = selection || [value.length, value.length];
            DOM.dialogInput.setSelectionRange(start, end);
        } else {
            DOM.dialogOk.focus();
        }
    });
}

DOM.dialogCancel.addEventListener('click', () => DOM.dialog.close('cancel'));

const showAlert = (title, message = '') => showDialog({ title, message, cancel: false });

// Step 2: Fetch and render the tree
async function loadTree() {
    try {
        const res = await fetch('/api/tree');
        if (!res.ok) throw new Error('Failed to load tree');
        const rootNode = await res.json();

        DOM.tree.innerHTML = '';
        // Render the root's children directly; the "." row adds nothing
        (rootNode.children || []).forEach(child => renderNode(child, DOM.tree));
        highlightSelection();
    } catch (err) {
        console.error(err);
        DOM.tree.innerHTML = '<div class="tree-error">Error loading tree</div>';
    }
}

function renderNode(node, container) {
    const el = document.createElement('div');
    const arrow = node.isDir ? '▾ ' : '';
    el.textContent = arrow + node.name;
    el.className = 'tree-node ' + (node.isDir ? 'tree-dir' : 'tree-file');
    el.dataset.path = node.path;

    let childrenContainer = null;

    if (node.children) {
        childrenContainer = document.createElement('div');
        childrenContainer.style.paddingLeft = '10px';
        node.children.forEach(child => renderNode(child, childrenContainer));
    }

    el.addEventListener('click', (e) => {
        e.stopPropagation(); // Prevent clicking a child from bubbling to parent folders

        if (node.isDir) select(node.path, true);

        if (node.isDir && childrenContainer) {
            // Toggle directory expansion
            const isHidden = childrenContainer.style.display === 'none';
            childrenContainer.style.display = isHidden ? 'block' : 'none';
            el.textContent = (isHidden ? '▾ ' : '▸ ') + node.name;
        } else if (!node.isDir) {
            // Open the file in a tab (or switch to it if already open)
            openFile(node.path);
        }
    });

    container.appendChild(el);
    if (childrenContainer) {
        container.appendChild(childrenContainer);
    }
}
// Theme toggle. The <head> script already applied the saved/system theme;
// a click saves an explicit choice, and with no saved choice we keep
// following the system setting live.
const btnTheme = document.getElementById('btn-theme');
const systemDark = matchMedia('(prefers-color-scheme: dark)');

function applyTheme(theme) {
    document.documentElement.dataset.theme = theme;
    const label = theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme';
    btnTheme.title = label;
    btnTheme.setAttribute('aria-label', label);
}

btnTheme.addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem('txt-theme', next); } catch (e) { /* private mode: still toggles */ }
    applyTheme(next);
});

systemDark.addEventListener('change', (e) => {
    let saved = null;
    try { saved = localStorage.getItem('txt-theme'); } catch (err) {}
    if (!saved) applyTheme(e.matches ? 'dark' : 'light');
});

applyTheme(document.documentElement.dataset.theme);

// "+" menu: New file / New folder
const btnNew = document.getElementById('btn-new');
const newMenu = document.getElementById('new-menu');

function setMenuOpen(open) {
    newMenu.hidden = !open;
    btnNew.setAttribute('aria-expanded', String(open));
    if (open) newMenu.querySelector('button').focus();
}

btnNew.addEventListener('click', () => setMenuOpen(newMenu.hidden));
// Picking an item closes the menu; the item's own handler (below) runs too
newMenu.addEventListener('click', () => setMenuOpen(false));
// Capture phase, so clicks on tree nodes (which stop propagation) still close it
document.addEventListener('click', (e) => {
    if (!newMenu.hidden && !newMenu.contains(e.target) && !btnNew.contains(e.target)) {
        setMenuOpen(false);
    }
}, true);
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !newMenu.hidden) {
        setMenuOpen(false);
        btnNew.focus();
    }
});

// Clicking blank sidebar space selects the workspace root
// (node clicks stop propagation, so they never reach here)
DOM.tree.addEventListener('click', () => select('.', true));

// ---------------------------------------------------------------------------
// Tabs
// Each tab owns its own <textarea>, so undo history, cursor and scroll
// position survive switching tabs.
// ---------------------------------------------------------------------------

// An untitled tab has never been saved, so it always counts as unsaved.
function isDirty(tab) {
    return tab.path === null || tab.editor.value !== tab.savedContent;
}

// Would closing this tab lose anything? (An empty untitled tab loses nothing.)
function hasUnsavedWork(tab) {
    return tab.path === null ? tab.editor.value !== '' : isDirty(tab);
}

// "untitled.txt", then "untitled-2.txt", ... avoiding names already in use
function nextUntitledName() {
    const used = new Set(state.tabs.filter(t => t.path === null).map(t => t.name));
    let name = 'untitled.txt';
    for (let n = 2; used.has(name); n++) name = `untitled-${n}.txt`;
    return name;
}

// `content` is the text on disk; `text` is what the editor shows. They differ
// only when restoring a session with unsaved edits.
function createTab({ path = null, name, content = '', text = content, version = '', lineEnding = 'lf' }) {
    const editor = document.createElement('textarea');
    editor.className = 'editor';
    editor.wrap = 'off';
    editor.spellcheck = false;
    editor.placeholder = 'Start writing…';
    editor.value = text;
    editor.hidden = true;

    const el = document.createElement('div');
    el.className = 'tab';
    el.setAttribute('role', 'tab');
    const nameEl = document.createElement('span');
    nameEl.className = 'tab-name';
    const closeBtn = document.createElement('button');
    closeBtn.className = 'tab-close';
    closeBtn.type = 'button';
    closeBtn.setAttribute('aria-label', 'Close tab');
    closeBtn.title = 'Close (Alt+W)';
    el.append(nameEl, closeBtn);

    // `view` holds scroll position while the tab is hidden (a hidden
    // textarea reports scrollTop 0) and a restored position until first shown
    const tab = { id: state.nextTabId++, path, name, editor, el, savedContent: content, version, lineEnding, view: null };

    el.draggable = true;
    el.addEventListener('dragstart', (e) => {
        draggedTab = tab;
        e.dataTransfer.effectAllowed = 'move';
        // A private type: Firefox needs some data to start a drag, and plain
        // text would get pasted if the tab were dropped onto an editor
        e.dataTransfer.setData('application/x-txt-tab', String(tab.id));
        el.classList.add('dragging');
    });
    el.addEventListener('dragend', () => {
        el.classList.remove('dragging');
        draggedTab = null;
        scheduleSessionSave();
    });

    el.addEventListener('click', () => activateTab(tab));
    // Middle-click closes, like most editors and browsers
    el.addEventListener('auxclick', (e) => { if (e.button === 1) closeTab(tab); });
    el.addEventListener('mousedown', (e) => { if (e.button === 1) e.preventDefault(); }); // no autoscroll
    closeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        closeTab(tab);
    });
    editor.addEventListener('input', () => {
        renderTab(tab);
        updateStatusBar();
        scheduleSessionSave();
    });
    editor.addEventListener('scroll', scheduleSessionSave, { passive: true });

    state.tabs.push(tab);
    DOM.tabs.appendChild(el);
    DOM.editors.appendChild(editor);
    renderTab(tab);
    return tab;
}

// Sync a tab's label and unsaved marker with its state
function renderTab(tab) {
    tab.el.querySelector('.tab-name').textContent = tab.name;
    tab.el.title = tab.path || `${tab.name} (not saved yet)`;
    tab.el.classList.toggle('dirty', isDirty(tab));
    tab.el.classList.toggle('untitled', tab.path === null);
    tab.el.classList.toggle('active', tab === state.activeTab);
    tab.el.setAttribute('aria-selected', String(tab === state.activeTab));
}

function activateTab(tab) {
    const prev = state.activeTab;
    state.activeTab = tab;
    if (prev && prev !== tab) {
        prev.view = { scrollTop: prev.editor.scrollTop, scrollLeft: prev.editor.scrollLeft };
        prev.editor.hidden = true;
        renderTab(prev);
    }
    tab.editor.hidden = false;
    if (tab.view) {
        tab.editor.scrollTop = tab.view.scrollTop;
        tab.editor.scrollLeft = tab.view.scrollLeft;
        tab.view = null;
    }
    renderTab(tab);
    tab.el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    tab.editor.focus({ preventScroll: true });
    if (tab.path) select(tab.path, false);
    updateStatusBar();
    scheduleSessionSave();
}

function newUntitledTab() {
    activateTab(createTab({ name: nextUntitledName() }));
}

async function closeTab(tab) {
    if (hasUnsavedWork(tab)) {
        activateTab(tab); // show what is about to be discarded
        const discard = await showDialog({
            title: `Close ${tab.name}?`,
            message: tab.path === null
                ? "This tab has never been saved. Its text will be lost."
                : "Your unsaved changes will be lost.",
            okLabel: "Discard",
            danger: true
        });
        if (!discard) {
            tab.editor.focus();
            return;
        }
    }

    const i = state.tabs.indexOf(tab);
    if (i === -1) return; // already closed
    state.tabs.splice(i, 1);
    tab.el.remove();
    tab.editor.remove();

    if (state.activeTab === tab) {
        state.activeTab = null;
        // Like Sublime: move to the tab on the right, else the one on the left.
        // Closing the last tab leaves a fresh untitled one.
        const next = state.tabs[i] || state.tabs[i - 1];
        if (next) activateTab(next);
        else newUntitledTab();
    }
    scheduleSessionSave();
}

async function openFile(path) {
    // Already open? Just switch to it.
    const existing = state.tabs.find(t => t.path === path);
    if (existing) {
        activateTab(existing);
        return;
    }

    try {
        const res = await fetch(`/api/file?path=${encodeURIComponent(path)}`);
        if (res.status === 413) {
            showAlert("File too large", `${path} is over the 4 MB limit.`);
            return;
        }
        if (res.status === 415) {
            showAlert("Can't open file", `${path} is not valid UTF-8 text.`);
            return;
        }
        if (!res.ok) throw new Error('Failed to fetch file');

        const data = await res.json();

        // A double-click could have opened it while we were fetching
        const raced = state.tabs.find(t => t.path === path);
        if (raced) {
            activateTab(raced);
            return;
        }

        // An empty, untouched untitled tab is just a placeholder: replace it
        const placeholder = state.activeTab;
        const replace = placeholder && placeholder.path === null && placeholder.editor.value === '';

        const tab = createTab({
            path,
            name: path.split('/').pop(),
            content: data.content,
            version: data.version,
            lineEnding: data.lineEnding
        });
        activateTab(tab);
        if (replace) closeTab(placeholder);
    } catch (err) {
        console.error(err);
        showAlert("Error opening file", path);
    }
}

function updateStatusBar() {
    const tab = state.activeTab;
    if (!tab) return;

    const dirty = isDirty(tab);
    DOM.statPath.textContent = tab.path || tab.name;
    DOM.statDirty.textContent = dirty ? "Unsaved •" : "Saved";
    DOM.statDirty.classList.toggle('unsaved', dirty);
    document.title = (dirty ? "• " : "") + tab.name + " - txt";

    // Line and character counts
    const content = tab.editor.value;
    const lines = content === "" ? 0 : content.split('\n').length;
    DOM.statCounts.textContent = `${lines} lines | ${content.length} chars`;
}

// Drag to reorder: the dragged tab moves live as it passes the middle of
// another tab. Moving only when crossing the midpoint in the direction of
// travel stops tabs of different widths from flickering back and forth.
let draggedTab = null;

DOM.tabs.addEventListener('dragover', (e) => {
    if (!draggedTab) return;
    e.preventDefault(); // allow dropping here
    e.dataTransfer.dropEffect = 'move';

    const target = e.target.closest('.tab');
    if (!target || target === draggedTab.el) return;

    const rect = target.getBoundingClientRect();
    const pastMiddle = e.clientX > rect.left + rect.width / 2;
    const draggedIsBefore = draggedTab.el.compareDocumentPosition(target) & Node.DOCUMENT_POSITION_FOLLOWING;

    if (draggedIsBefore && pastMiddle) {
        target.after(draggedTab.el);
    } else if (!draggedIsBefore && !pastMiddle) {
        target.before(draggedTab.el);
    } else {
        return;
    }
    // Keep state.tabs in tab-bar order (used to pick a neighbour on close)
    const order = [...DOM.tabs.children];
    state.tabs.sort((a, b) => order.indexOf(a.el) - order.indexOf(b.el));
});

DOM.tabs.addEventListener('drop', (e) => {
    if (draggedTab) e.preventDefault();
});

// "+" in the tab bar, or double-click on empty tab-bar space (as in Sublime)
document.getElementById('btn-new-tab').addEventListener('click', newUntitledTab);
DOM.tabbar.addEventListener('dblclick', (e) => {
    if (e.target === DOM.tabbar || e.target === DOM.tabs) newUntitledTab();
});

// Keys inside any editor
DOM.editors.addEventListener('keydown', (e) => {
    // Insert a real tab character instead of moving focus.
    // insertText keeps the browser's undo history intact.
    if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey) {
        e.preventDefault();
        if (!document.execCommand('insertText', false, '\t')) {
            e.target.setRangeText('\t', e.target.selectionStart, e.target.selectionEnd, 'end');
            e.target.dispatchEvent(new Event('input'));
        }
    }
});

// Global shortcuts. Cmd/Ctrl+N, W and T belong to the browser and can't be
// overridden, so new/close tab use Alt instead. e.code is used because on
// macOS Alt+letter produces a special character in e.key.
document.addEventListener('keydown', (e) => {
    if (DOM.dialog.open) return;

    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 's') {
        e.preventDefault(); // Stop the browser's "Save Webpage" dialog
        if (state.activeTab) saveTab(state.activeTab);
        return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey) {
        if (e.code === 'KeyN') {
            e.preventDefault();
            newUntitledTab();
        } else if (e.code === 'KeyW') {
            e.preventDefault();
            if (state.activeTab) closeTab(state.activeTab);
        }
    }
});

// ---------------------------------------------------------------------------
// Session: open tabs and unsaved text survive reloads and restarts.
// Stored by the server as .txt-session.json in the workspace root.
// ---------------------------------------------------------------------------

const SESSION_SAVE_DELAY = 800;      // ms after the last change
const KEEPALIVE_LIMIT = 60 * 1024;   // browsers cap keepalive request bodies at 64 KB

let sessionReady = false;   // don't overwrite the stored session before it's restored
let sessionTimer = null;
let sessionChanges = 0;     // bumped on every change...
let sessionPersisted = 0;   // ...and recorded here once a save of that change lands

function sessionSnapshot() {
    return {
        version: 1,
        active: state.tabs.indexOf(state.activeTab),
        tabs: state.tabs.map(tab => {
            const view = tab === state.activeTab || !tab.view
                ? { scrollTop: tab.editor.scrollTop, scrollLeft: tab.editor.scrollLeft }
                : tab.view;
            const entry = {
                path: tab.path,
                name: tab.name,
                lineEnding: tab.lineEnding,
                selection: [tab.editor.selectionStart, tab.editor.selectionEnd],
                scroll: [view.scrollTop, view.scrollLeft]
            };
            // Only unsaved text is stored; saved files reload from disk
            if (hasUnsavedWork(tab)) {
                entry.text = tab.editor.value;
                entry.version = tab.version; // the disk version these edits are based on
            }
            return entry;
        })
    };
}

function scheduleSessionSave() {
    if (!sessionReady) return;
    sessionChanges++;
    clearTimeout(sessionTimer);
    sessionTimer = setTimeout(saveSession, SESSION_SAVE_DELAY);
}

async function saveSession({ keepalive = false } = {}) {
    clearTimeout(sessionTimer);
    if (!sessionReady || sessionPersisted === sessionChanges) return;
    const upTo = sessionChanges;
    const body = JSON.stringify(sessionSnapshot());
    if (keepalive && body.length > KEEPALIVE_LIMIT) return;
    try {
        const res = await fetch('/api/session', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body,
            keepalive
        });
        if (res.ok) sessionPersisted = Math.max(sessionPersisted, upTo);
    } catch (err) {
        console.error("Saving session failed:", err);
    }
}

// Flush when the page is hidden (switching apps, closing the tab)...
document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') saveSession(); // page is still alive: no size limit
});
// ...and when leaving. Only warn if unsaved text might not have reached disk.
window.addEventListener('beforeunload', (e) => {
    if (sessionPersisted === sessionChanges) return;
    const body = JSON.stringify(sessionSnapshot());
    if (body.length <= KEEPALIVE_LIMIT) {
        saveSession({ keepalive: true }); // small enough to finish after the page is gone
    } else if (state.tabs.some(hasUnsavedWork)) {
        e.preventDefault();
        e.returnValue = '';
    }
});

// Rebuild the tabs from the stored session. Saved files are re-read from disk
// (picking up outside edits); tabs with unsaved edits get their text back.
async function restoreSession() {
    let session = null;
    try {
        const res = await fetch('/api/session');
        if (res.ok) session = await res.json();
    } catch (err) {
        console.error("Loading session failed:", err);
    }
    const entries = session && Array.isArray(session.tabs) ? session.tabs : [];

    // Read all files in parallel, then create tabs in their original order
    const specs = await Promise.all(entries.map(async (s) => {
        const hasText = typeof s.text === 'string';
        if (s.path == null) {
            // Untitled: worth restoring only if it had text
            return hasText && s.text !== '' ? { name: s.name || 'untitled.txt', text: s.text } : null;
        }

        let disk = null;
        try {
            const res = await fetch(`/api/file?path=${encodeURIComponent(s.path)}`);
            if (res.ok) disk = await res.json();
        } catch (err) { /* treated as missing */ }

        const name = s.path.split('/').pop();
        if (!disk) {
            // Gone from disk: keep unsaved text as an untitled tab, else drop it
            return hasText ? { name, text: s.text } : null;
        }
        return {
            path: s.path,
            name,
            content: disk.content,
            text: hasText ? s.text : disk.content,
            // Edits keep the version they were based on, so if the file changed
            // on disk since, saving reports a conflict instead of overwriting
            version: hasText && s.version ? s.version : disk.version,
            lineEnding: disk.lineEnding
        };
    }));

    let toActivate = null;
    specs.forEach((spec, i) => {
        if (!spec) return;
        if (spec.path === undefined) spec.path = null;
        // Untitled names must stay unique among restored tabs
        if (spec.path === null && state.tabs.some(t => t.path === null && t.name === spec.name)) {
            spec.name = nextUntitledName();
        }
        const tab = createTab(spec);
        const s = entries[i];
        if (Array.isArray(s.selection)) {
            const len = tab.editor.value.length;
            tab.editor.setSelectionRange(Math.min(s.selection[0], len), Math.min(s.selection[1], len));
        }
        if (Array.isArray(s.scroll)) tab.view = { scrollTop: s.scroll[0], scrollLeft: s.scroll[1] };
        if (i === session.active || !toActivate) toActivate = tab;
    });

    if (toActivate) activateTab(toActivate);
    else newUntitledTab();
    sessionReady = true;
}

// ---------------------------------------------------------------------------
// Saving
// ---------------------------------------------------------------------------

function saveTab(tab) {
    if (tab.path === null) return saveUntitled(tab);
    if (!isDirty(tab)) return;
    return writeTab(tab, tab.path, tab.version);
}

// PUT the tab's text to `path`. An empty baseVersion means "create a new file";
// the server refuses (409) if something already exists there.
// Resolves to true on success.
async function writeTab(tab, path, baseVersion) {
    const content = tab.editor.value;
    if (tab === state.activeTab) DOM.statDirty.textContent = "Saving...";

    try {
        const res = await fetch(`/api/file?path=${encodeURIComponent(path)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content, baseVersion, lineEnding: tab.lineEnding })
        });

        if (res.status === 409) {
            if (baseVersion === '') {
                showAlert("File already exists", `${path} already exists. Choose another name.`);
            } else {
                showAlert("File changed on disk", "This file was modified outside txt. Copy your changes, reopen the file, and merge them.");
            }
            return false;
        }
        if (res.status === 404) {
            showAlert("Folder not found", `The folder for ${path} doesn't exist. Create it first with + → New folder.`);
            return false;
        }
        if (!res.ok) throw new Error((await res.text()).trim() || `HTTP ${res.status}`);

        const data = await res.json();

        // Record what was saved; typing during the request stays unsaved
        tab.savedContent = content;
        tab.version = data.version;
        return true;
    } catch (err) {
        console.error("Save failed:", err);
        showAlert("Failed to save file", String(err.message || err));
        return false;
    } finally {
        renderTab(tab);
        if (tab === state.activeTab) updateStatusBar();
        scheduleSessionSave(); // saved tabs no longer need their text in the session
    }
}

// "Save as" for a tab that has never been saved
async function saveUntitled(tab) {
    const prefix = selectedDirPrefix();
    const stem = tab.name.replace(/\.txt$/, '');
    let path = await showDialog({
        title: "Save as",
        message: "Path relative to the workspace. .txt is added if you leave it off.",
        input: true,
        value: prefix + tab.name,
        // Pre-select the name (not the folder or extension) so typing replaces it
        selection: [prefix.length, prefix.length + stem.length],
        placeholder: "notes/idea.txt",
        okLabel: "Save"
    });
    tab.editor.focus();
    if (!path) return;

    if (!/\.[^/]*$/.test(path)) path += '.txt';
    if (state.tabs.some(t => t.path === path)) {
        showAlert("File already open", `${path} is already open in another tab.`);
        return;
    }

    if (await writeTab(tab, path, '')) {
        tab.path = path;
        tab.name = path.split('/').pop();
        renderTab(tab);
        updateStatusBar();
        scheduleSessionSave();
        select(path, false);
        await loadTree();
    }
}

// ---------------------------------------------------------------------------
// Sidebar "+" menu actions
// ---------------------------------------------------------------------------

// New file: create it on disk, then open it in a tab
document.getElementById('btn-new-file').addEventListener('click', async () => {
    const path = await showDialog({
        title: "New file",
        message: "Path relative to the workspace. .txt is added if you leave it off.",
        input: true,
        value: selectedDirPrefix(),
        placeholder: "notes/idea.txt",
        okLabel: "Create"
    });
    if (!path) return; // User cancelled or entered nothing

    try {
        const res = await fetch(`/api/file?path=${encodeURIComponent(path)}`, {
            method: 'POST'
        });

        if (res.status === 409) {
            showAlert("File already exists", path);
            return;
        }
        if (!res.ok) {
            const errorMsg = await res.text();
            showAlert("Couldn't create file", errorMsg);
            return;
        }

        // Reload the sidebar to show the new file
        await loadTree();
        // Automatically open the newly created file!
        // (Ensure it ends in .txt since the backend appends it if missing)
        const finalPath = path.endsWith('.txt') ? path : path + '.txt';
        openFile(finalPath);

    } catch (err) {
        console.error(err);
        showAlert("Error creating file", String(err.message || err));
    }
});

// New folder
document.getElementById('btn-new-dir').addEventListener('click', async () => {
    const path = await showDialog({
        title: "New folder",
        message: "Path relative to the workspace. The parent folder must already exist.",
        input: true,
        value: selectedDirPrefix(),
        placeholder: "archive/2026",
        okLabel: "Create"
    });
    if (!path) return;

    try {
        const res = await fetch(`/api/dir?path=${encodeURIComponent(path)}`, {
            method: 'POST'
        });

        if (res.status === 409) {
            showAlert("Folder already exists", path);
            return;
        }
        if (!res.ok) {
            const errorMsg = await res.text();
            showAlert("Couldn't create folder", errorMsg);
            return;
        }

        // Reload the sidebar to show the new folder, and select it so
        // "+ File" creates inside it next
        state.selectedPath = path;
        state.selectedIsDir = true;
        await loadTree();

    } catch (err) {
        console.error(err);
        showAlert("Error creating folder", String(err.message || err));
    }
});

// Initial load: restore the last session (or start with an empty untitled
// tab). If txt was started with a file (`txt notes.txt`), open it too.
loadTree();
const initialFile = new URLSearchParams(location.search).get('open');
if (initialFile) history.replaceState(null, '', '/'); // a reload shouldn't reopen it
restoreSession().then(() => {
    if (initialFile) openFile(initialFile);
});
