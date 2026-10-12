// View-only display for files txt can't edit safely: over 4 MB (up to several
// GB), or not valid UTF-8.
// The whole file is never loaded: a window of about 1 MB, cut at line
// boundaries, is fetched with an HTTP Range request, and only the lines on
// screen are drawn. The scrollbar maps to byte offset rather than line number,
// so any point in the file is one fetch away and no line count is needed.
// Search (Cmd/Ctrl+F) runs on the server (/api/find, /api/count); the viewer
// jumps to each match and highlights matches in the lines on screen.

const LINE_HEIGHT = 20;   // px, must match .viewer-lines in style.css
const PAD_Y = 8;          // px, vertical padding of .viewer-lines
const WINDOW = 1 << 20;   // bytes per fetched window
const NL = 10;
const decoder = new TextDecoder(); // invalid UTF-8 shows as U+FFFD

export function createViewer(path, size, { onChange = () => {}, onError = () => {} } = {}) {
    const el = document.createElement('div');
    el.className = 'viewer';
    el.tabIndex = 0;
    const linesEl = document.createElement('div');
    linesEl.className = 'viewer-lines';
    const track = document.createElement('div');
    track.className = 'viewer-track';
    const thumb = document.createElement('div');
    thumb.className = 'viewer-thumb';
    track.append(thumb);
    const bar = document.createElement('div');
    bar.className = 'viewer-search';
    bar.hidden = true;
    bar.innerHTML = `
        <input type="search" placeholder="Find in file" spellcheck="false" maxlength="1024" aria-label="Find in file">
        <button type="button" class="icon-btn vs-case" aria-pressed="false" title="Match case">Aa</button>
        <span class="vs-status" aria-live="polite"></span>
        <button type="button" class="icon-btn vs-prev" title="Previous match (Shift+Enter)" aria-label="Previous match">
            <svg viewBox="0 0 24 24"><path d="M6 15l6-6 6 6"/></svg></button>
        <button type="button" class="icon-btn vs-next" title="Next match (Enter)" aria-label="Next match">
            <svg viewBox="0 0 24 24"><path d="M6 9l6 6 6-6"/></svg></button>
        <button type="button" class="icon-btn vs-close" title="Close (Esc)" aria-label="Close search">
            <svg viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18"/></svg></button>`;
    const input = bar.querySelector('input');
    const caseBtn = bar.querySelector('.vs-case');
    const statusEl = bar.querySelector('.vs-status');
    el.append(bar, linesEl, track);

    const url = `/api/raw?path=${encodeURIComponent(path)}`;

    // The loaded window: `bytes` starts at file offset `start`; `lines` holds
    // the index in `bytes` where each complete line begins.
    let win = { start: 0, bytes: new Uint8Array(0), lines: [] };
    let top = 0;          // index in win.lines of the first line on screen
    let loadSeq = 0;      // only the newest fetch may replace the window
    let loading = false;

    const winEnd = () => win.start + win.bytes.length;
    const abs = (i) => win.start + win.lines[i];
    const visible = () => Math.max(1, Math.floor((linesEl.clientHeight - 2 * PAD_Y) / LINE_HEIGHT));

    // Fetch the window around `center`. `anchor` is read just before the new
    // window replaces the old one, so scrolling during the fetch isn't lost;
    // its line ends up `lead` lines below the top of the screen.
    async function load(center, anchor = () => center, lead = 0) {
        const seq = ++loadSeq;
        loading = true;
        const start = Math.max(0, Math.min(center - WINDOW / 2, size - WINDOW));
        const end = Math.min(size, start + WINDOW);
        let bytes;
        try {
            const res = await fetch(url, { headers: { Range: `bytes=${start}-${end - 1}` } });
            if (!res.ok) throw new Error(res.status === 401 ? 'not signed in' : `HTTP ${res.status}`);
            bytes = new Uint8Array(await res.arrayBuffer());
        } catch (err) {
            if (seq === loadSeq) {
                loading = false;
                onError(err);
            }
            return;
        }
        if (seq !== loadSeq) return;
        loading = false;

        // Cut to whole lines: drop the partial line at each edge, unless the
        // window sits inside one line longer than itself.
        let first = 0;
        if (start > 0) {
            const nl = bytes.indexOf(NL);
            if (nl !== -1) first = nl + 1;
        }
        if (start + bytes.length < size) {
            const nl = bytes.lastIndexOf(NL);
            if (nl >= first) bytes = bytes.subarray(0, nl + 1);
        }
        const lines = [first];
        for (let i = bytes.indexOf(NL, first); i !== -1 && i + 1 < bytes.length; i = bytes.indexOf(NL, i + 1)) {
            lines.push(i + 1);
        }

        const target = anchor();
        win = { start, bytes, lines };
        top = lineAt(target) - lead;
        render();
    }

    // Index of the line containing file offset `offset` (binary search)
    function lineAt(offset) {
        let lo = 0, hi = win.lines.length - 1;
        while (lo < hi) {
            const mid = (lo + hi + 1) >> 1;
            if (abs(mid) <= offset) lo = mid; else hi = mid - 1;
        }
        return lo;
    }

    function lineText(i) {
        let end = i + 1 < win.lines.length ? win.lines[i + 1] - 1 : win.bytes.length;
        if (end > win.lines[i] && win.bytes[end - 1] === NL) end--;
        if (end > win.lines[i] && win.bytes[end - 1] === 13) end--; // CRLF
        return decoder.decode(win.bytes.subarray(win.lines[i], end));
    }

    // Keep the last screen full at the end of the file
    function clampTop() {
        const max = winEnd() === size ? Math.max(0, win.lines.length - visible()) : win.lines.length - 1;
        top = Math.max(0, Math.min(top, max));
    }

    function render() {
        clampTop();
        const end = Math.min(win.lines.length, top + visible() + 1);
        if (!query) {
            const out = [];
            for (let i = top; i < end; i++) out.push(lineText(i));
            linesEl.textContent = out.join('\n');
        } else {
            linesEl.replaceChildren(highlighted(top, end));
        }
        const atEnd = winEnd() === size && top + visible() >= win.lines.length;
        const fraction = !win.lines.length ? 0 : atEnd ? 1 : abs(top) / size;
        thumb.style.top = `${fraction * (track.clientHeight - thumb.offsetHeight)}px`;
        onChange();
    }

    function scrollBy(n) {
        if (!win.lines.length) return;
        top += n;
        clampTop();
        render();
        // Near either edge of the window (and not the file)? Re-centre on the
        // current line in the background; scrolling stays within loaded lines.
        const margin = 2 * visible() + 50;
        const nearStart = top < margin && win.start > 0;
        const nearEnd = top + visible() + margin > win.lines.length && winEnd() < size;
        if ((nearStart || nearEnd) && !loading) load(abs(top), () => abs(top));
    }

    const jump = (offset) => load(Math.max(0, Math.min(size, offset)));

    // ---- Search ----------------------------------------------------------
    // The server finds matches by byte offset; `match` is the current one.
    // Case-insensitive means ASCII letters only, same as the server.

    let query = null;         // { q, fold } while the search bar has text
    let match = -1;           // file offset of the current match
    let count = null;         // matches in the whole file, once counted
    let noMatch = false;
    let findCtl = null, countCtl = null;  // abort a search a newer one replaces

    const asciiLower = (str) => str.replace(/[A-Z]+/g, (c) => c.toLowerCase());

    // Lines top..end-1 with every match wrapped in <mark>; the current one
    // gets class "current".
    function highlighted(from, end) {
        const frag = document.createDocumentFragment();
        const needle = query.fold ? asciiLower(query.q) : query.q;
        for (let i = from; i < end; i++) {
            if (i > from) frag.append('\n');
            const text = lineText(i);
            const hay = query.fold ? asciiLower(text) : text;
            // Character index of the current match, if it's on this line
            const onLine = match >= abs(i) && (i + 1 >= win.lines.length || match < abs(i + 1));
            const cur = onLine ? decoder.decode(win.bytes.subarray(win.lines[i], match - win.start)).length : -1;
            let pos = 0;
            for (let j = hay.indexOf(needle); j !== -1; j = hay.indexOf(needle, j + needle.length)) {
                const mark = document.createElement('mark');
                if (j === cur) mark.className = 'current';
                mark.textContent = text.slice(j, j + needle.length);
                frag.append(text.slice(pos, j), mark);
                pos = j + needle.length;
            }
            frag.append(text.slice(pos));
        }
        return frag;
    }

    async function api(endpoint, params, ctl) {
        const qs = new URLSearchParams({ path, q: query.q, ...(query.fold ? {} : { case: '1' }), ...params });
        const res = await fetch(`/api/${endpoint}?${qs}`, { signal: ctl.signal });
        if (!res.ok) throw new Error(res.status === 401 ? 'not signed in' : (await res.text()).trim() || `HTTP ${res.status}`);
        return res.json();
    }

    function updateSearchStatus() {
        statusEl.textContent = !query ? ''
            : noMatch || count === 0 ? 'No matches'
            : count === null ? 'Searching…'
            : `${count.toLocaleString()} ${count === 1 ? 'match' : 'matches'}`;
    }

    // A new query (typed, or the case toggle): count in the background and
    // jump to the first match from the top of the screen
    function startSearch() {
        findCtl?.abort();
        countCtl?.abort();
        match = -1;
        count = null;
        noMatch = false;
        query = input.value && !bar.hidden ? { q: input.value, fold: caseBtn.getAttribute('aria-pressed') !== 'true' } : null;
        render();
        updateSearchStatus();
        if (!query) return;
        const mine = query;
        countCtl = new AbortController();
        api('count', {}, countCtl).then((d) => {
            if (query !== mine) return;
            count = d.count;
            updateSearchStatus();
        }).catch((err) => { if (err.name !== 'AbortError') onError(err); });
        find(false);
    }

    async function find(backward) {
        if (!query) return;
        findCtl?.abort();
        findCtl = new AbortController();
        const mine = query;
        const from = match < 0 ? (win.lines.length ? abs(top) : 0) : backward ? match : match + 1;
        let offset;
        try {
            ({ offset } = await api('find', { from, ...(backward ? { dir: 'prev' } : {}) }, findCtl));
        } catch (err) {
            if (err.name !== 'AbortError') onError(err);
            return;
        }
        if (query !== mine) return;
        noMatch = offset < 0;
        updateSearchStatus();
        if (noMatch) return;
        match = offset;
        showMatch();
    }

    // Bring the current match on screen, a third of the way down, unless it's
    // already visible
    function showMatch() {
        const lead = Math.floor(visible() / 3);
        if (win.lines.length && match >= abs(0) && match < winEnd()) {
            const line = lineAt(match);
            if (line < top || line >= top + visible()) top = line - lead;
            scrollBy(0); // renders, and re-centres the window if near its edge
            revealMatch();
        } else {
            load(match, () => match, lead).then(revealMatch);
        }
    }

    // Scroll sideways if the current match is off to the left or right
    function revealMatch() {
        const mark = linesEl.querySelector('mark.current');
        if (!mark) return;
        const left = mark.offsetLeft, right = left + mark.offsetWidth;
        if (left < linesEl.scrollLeft || right > linesEl.scrollLeft + linesEl.clientWidth) {
            linesEl.scrollLeft = Math.max(0, left - 48);
        }
    }

    function openSearch() {
        const wasHidden = bar.hidden;
        bar.hidden = false;
        input.focus();
        input.select();
        if (wasHidden && input.value) startSearch();
    }

    function closeSearch() {
        bar.hidden = true;
        startSearch(); // hidden: clears the query and highlights
        el.focus({ preventScroll: true });
    }

    let typingTimer = null;
    input.addEventListener('input', () => {
        clearTimeout(typingTimer);
        typingTimer = setTimeout(startSearch, 120);
    });
    caseBtn.addEventListener('click', () => {
        caseBtn.setAttribute('aria-pressed', String(caseBtn.getAttribute('aria-pressed') !== 'true'));
        startSearch();
        input.focus();
    });
    bar.querySelector('.vs-next').addEventListener('click', () => find(false));
    bar.querySelector('.vs-prev').addEventListener('click', () => find(true));
    bar.querySelector('.vs-close').addEventListener('click', closeSearch);

    // Wheel: vertical scrolls lines; mostly-horizontal is left to the browser
    let wheelPx = 0;
    el.addEventListener('wheel', (e) => {
        if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
        e.preventDefault();
        const unit = e.deltaMode === 1 ? LINE_HEIGHT : e.deltaMode === 2 ? visible() * LINE_HEIGHT : 1;
        wheelPx += e.deltaY * unit;
        const n = Math.trunc(wheelPx / LINE_HEIGHT);
        wheelPx -= n * LINE_HEIGHT;
        if (n) scrollBy(n);
    }, { passive: false });

    el.addEventListener('keydown', (e) => {
        const mod = e.metaKey || e.ctrlKey;
        const key = e.key.toLowerCase();
        if (mod && !e.altKey && (key === 'f' || key === 'g')) {
            // Cmd/Ctrl+F: find (instead of the browser's, which only sees the
            // lines on screen); Cmd/Ctrl+G and Shift+G: next and previous
            e.preventDefault();
            if (key === 'f' || bar.hidden) openSearch();
            else find(e.shiftKey);
            return;
        }
        if (bar.contains(e.target)) {
            if (e.key === 'Enter' && e.target === input) {
                e.preventDefault();
                clearTimeout(typingTimer);
                // Enter right after typing runs the pending search first
                if (!query || query.q !== input.value) startSearch();
                else find(e.shiftKey);
            } else if (e.key === 'Escape') {
                e.preventDefault();
                closeSearch();
            }
            return; // typing in the search bar never scrolls
        }
        const page = Math.max(1, visible() - 1);
        const keys = {
            ArrowDown: () => mod ? jump(size) : scrollBy(1),
            ArrowUp: () => mod ? jump(0) : scrollBy(-1),
            PageDown: () => scrollBy(page),
            PageUp: () => scrollBy(-page),
            ' ': () => scrollBy(e.shiftKey ? -page : page),
            Home: () => jump(0),
            End: () => jump(size)
        };
        if (!keys[e.key] || e.altKey) return;
        e.preventDefault();
        keys[e.key]();
    });

    // Scrollbar: click the track to jump, drag the thumb to scrub
    const offsetAt = (clientY) => {
        const r = track.getBoundingClientRect();
        const f = (clientY - r.top - thumb.offsetHeight / 2) / Math.max(1, r.height - thumb.offsetHeight);
        return Math.round(Math.max(0, Math.min(1, f)) * size);
    };
    track.addEventListener('pointerdown', (e) => {
        e.preventDefault();
        el.focus({ preventScroll: true });
        track.setPointerCapture(e.pointerId);
        thumb.classList.add('dragging');
        jump(offsetAt(e.clientY));
    });
    let pendingY = null;
    track.addEventListener('pointermove', (e) => {
        if (!track.hasPointerCapture(e.pointerId)) return;
        // One fetch per frame at most while dragging; stale ones are dropped
        if (pendingY === null) requestAnimationFrame(() => { jump(offsetAt(pendingY)); pendingY = null; });
        pendingY = e.clientY;
    });
    track.addEventListener('lostpointercapture', () => thumb.classList.remove('dragging'));

    // Redraw when the size changes, including when the tab is shown
    new ResizeObserver(() => render()).observe(linesEl);

    load(0);

    return {
        el,
        // e.g. "View only · 3.92 GB · 41%"
        status() {
            const gb = size / 2 ** 30;
            const human = gb >= 1 ? `${gb.toFixed(2)} GB` : `${(size / 2 ** 20).toFixed(1)} MB`;
            const pct = size ? Math.round(100 * (win.lines.length ? abs(top) : 0) / size) : 0;
            return `${human} · ${pct}%`;
        }
    };
}
