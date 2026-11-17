// MovieStreamer for Samsung Tizen TVs: lists the videos served by the
// zerofin Go server and plays them with AVPlay. ES5 for older TV engines.
(function () {
  var av = window.webapis && webapis.avplay;
  var $ = function (id) { return document.getElementById(id); };
  var server = localStorage.getItem('server') || '';
  var lib = [], titles = [], tsel = 0, open = null, kind = 0, sortBy = 0;
  var rows = [], sel = 0;
  var COLS = 7, KINDS = ['All', 'Movies', 'Shows'], SORTS = ['Recently added', 'Title', 'Year'];
  var cur = null, subs = [], subIdx = 0, cues = null, audioIdx = 0, osdTimer = 0, subTimer = 0;
  var K = { enter: 13, back: 10009, left: 37, up: 38, right: 39, down: 40 };

  ['MediaPlayPause', 'MediaPlay', 'MediaPause', 'MediaStop', 'MediaFastForward', 'MediaRewind', 'ColorF0Red', 'ColorF1Green', 'ColorF2Yellow']
    .forEach(function (name) {
      try { tizen.tvinputdevice.registerKey(name); K[name] = tizen.tvinputdevice.getKey(name).code; } catch (e) {}
    });

  function show(id) {
    ['setup', 'browse', 'title', 'player'].forEach(function (s) { $(s).className = 'screen' + (s === id ? ' on' : ''); });
  }

  function base() { return 'http://' + server.replace(/^https?:\/\//, '').replace(/\/+$/, ''); }

  function get(url, cb) {
    var x = new XMLHttpRequest();
    x.open('GET', url);
    x.timeout = 5000;
    x.onload = function () { cb(x.status === 200 ? x.responseText : null); };
    x.onerror = x.ontimeout = function () { cb(null); };
    x.send();
  }

  // Setup: one text field for host:port, kept in localStorage.
  function setup(msg) {
    show('setup');
    $('err').textContent = msg || '';
    $('addr').value = server || '10.0.0.';
    $('addr').focus();
  }

  // load fetches the library, then shows the open title again, or the grid.
  function load() {
    get(base() + '/api/library', function (text) {
      var data;
      try { data = JSON.parse(text); } catch (e) {}
      if (!text || !data) return setup('Could not reach ' + base() + '/api/library');
      lib = data;
      $('title').textContent = 'MovieStreamer · ' + server;
      var t = null;
      lib.forEach(function (x) { if (x.id === open) t = x; });
      if (t) return openTitle(t);
      open = null;
      grid();
      show('browse');
    });
  }

  // grid draws the poster tiles, filtered by kind and sorted. A tile shows the
  // title as text when it has no poster.
  function grid() {
    var prev = titles[tsel] && titles[tsel].id;
    titles = lib.filter(function (t) { return !kind || (kind === 2) === t.show; });
    titles.sort(function (a, b) {
      var d = sortBy === 0 ? b.added - a.added : sortBy === 2 ? b.year - a.year : 0;
      return d || a.title.toLowerCase().localeCompare(b.title.toLowerCase());
    });
    titles.forEach(function (t, i) { if (t.id === prev) tsel = i; });
    tsel = Math.max(0, Math.min(tsel, titles.length - 1));
    $('hint').textContent = 'OK open · Green show: ' + KINDS[kind] + ' · Yellow sort: ' + SORTS[sortBy] + ' · Red change server · Back exit';
    var g = $('grid');
    g.innerHTML = '';
    if (!titles.length) g.textContent = 'No videos.';
    titles.forEach(function (t) {
      var d = document.createElement('div'), p = document.createElement('div'), n = document.createElement('div');
      d.className = 'tile';
      p.className = 'poster';
      p.textContent = t.title;
      if (t.poster) {
        var img = document.createElement('img');
        img.onerror = function () { p.removeChild(img); };
        img.src = base() + t.poster;
        p.appendChild(img);
      }
      n.className = 'tname';
      n.textContent = t.title + (t.year ? ' (' + t.year + ')' : '');
      d.appendChild(p);
      d.appendChild(n);
      g.appendChild(d);
    });
    pick(tsel);
  }

  // pick moves the grid selection to tile i and scrolls its row to the top.
  function pick(i) {
    var g = $('grid');
    if (!titles[i]) return;
    if (g.children[tsel]) g.children[tsel].className = 'tile';
    tsel = i;
    g.children[i].className = 'tile sel';
    g.scrollTop = g.children[i].offsetTop - g.offsetTop - 8;
  }

  // openTitle lists the videos of a title, with a header row per season.
  function openTitle(t) {
    var prev = open === t.id && rows[sel] && rows[sel].path, season = null;
    if (open !== t.id) sel = 0;
    open = t.id;
    rows = [];
    t.videos.forEach(function (v) {
      if (t.show && v.season !== season) {
        season = v.season;
        rows.push({ dir: season ? 'Season ' + season : 'Specials' });
      }
      var name = t.show ? v.episode + ' · ' + (v.title || 'Episode ' + v.episode) : v.name;
      rows.push({
        name: name,
        full: t.show ? t.title + ' · S' + v.season + ' E' + name : t.title,
        path: v.url,
        subs: v.subs.map(function (s) { return s.url; })
      });
    });
    rows.forEach(function (r, i) { if (prev && r.path === prev) sel = i; });
    sel = Math.max(0, Math.min(sel, rows.length - 1));
    if (rows[sel] && rows[sel].dir) move(1);
    $('tname').textContent = t.title + (t.year ? ' (' + t.year + ')' : '');
    render();
    show('title');
  }

  function render() {
    var list = $('list');
    list.innerHTML = '';
    if (!rows.length) list.textContent = 'No videos.';
    rows.forEach(function (r, i) {
      var d = document.createElement('div');
      d.className = r.dir ? 'dir' : 'item' + (i === sel ? ' sel' : '');
      d.textContent = r.dir || r.name;
      list.appendChild(d);
    });
    var el = list.children[sel];
    if (el) list.scrollTop = Math.max(0, el.offsetTop - list.offsetTop - 380);
  }

  function move(step) {
    var i = sel + step;
    while (rows[i] && rows[i].dir) i += step;
    if (rows[i]) sel = i;
  }

  // Player.
  var pillTimer = 0, seekTarget = null, seekPresses = 0, lastPress = 0;

  function play(v) {
    cur = v;
    subs = [{ label: 'Off' }]; subIdx = 0; cues = null; audioIdx = 0; seekTarget = null;
    $('sub').textContent = '';
    $('name').textContent = v.full;
    show('player');
    pill('Loading…', true);
    try {
      av.close();
      av.open(base() + v.path);
      av.setDisplayRect(0, 0, 1920, 1080);
      av.setListener({
        oncurrentplaytime: tick,
        onbufferingstart: function () { pill('Buffering…', true); },
        onbufferingcomplete: function () { if ($('pilltext').textContent === 'Buffering…') hidePill(); },
        onstreamcompleted: function () { localStorage.removeItem('pos:' + cur.path); cur = null; stop(); },
        onerror: function (e) { pill('Error: ' + e, true); },
        onsubtitlechange: function (dur, text) {
          if (!subs[subIdx] || subs[subIdx].kind !== 'embedded') return;
          $('sub').textContent = text;
          clearTimeout(subTimer);
          subTimer = setTimeout(function () { $('sub').textContent = ''; }, +dur || 3000);
        }
      });
      av.prepareAsync(function () {
        var pos = +localStorage.getItem('pos:' + v.path) || 0;
        if (pos > 30000) av.seekTo(pos - 5000);
        av.play();
        v.subs.forEach(function (p) { subs.push({ kind: 'file', path: p, label: decodeURIComponent(p.split('/').pop()) }); });
        tracks('TEXT').forEach(function (t) { subs.push({ kind: 'embedded', index: t.index, label: t.label }); });
        var active = current('AUDIO');
        tracks('AUDIO').forEach(function (t, i) { if (t.index === active) audioIdx = i; });
        if (v.subs.length) setSub(1);
        pill(pos > 30000 ? 'Resumed at ' + fmt(pos - 5000) : 'Playing');
        osd();
      }, function (e) { pill('Cannot play: ' + e, true); });
    } catch (e) { pill('Cannot play: ' + e.message, true); }
  }

  // tracks lists the AUDIO or TEXT tracks with a readable label.
  function tracks(type) {
    var out = [], all = [];
    try { all = av.getTotalTrackInfo(); } catch (e) {}
    for (var i = 0; i < all.length; i++) {
      if (all[i].type !== type) continue;
      var info = {};
      try { info = JSON.parse(all[i].extra_info); } catch (e) {}
      var parts = [];
      if (info.language || info.track_lang) parts.push((info.language || info.track_lang).toUpperCase());
      if (info.fourCC) parts.push(info.fourCC);
      if (+info.channels) parts.push(info.channels + 'ch');
      out.push({ index: all[i].index, label: parts.join(' · ') || 'Track ' + (out.length + 1) });
    }
    return out;
  }

  function current(type) {
    var all = [];
    try { all = av.getCurrentStreamInfo(); } catch (e) {}
    for (var i = 0; i < all.length; i++) if (all[i].type === type) return all[i].index;
    return -1;
  }

  function now() { try { return av.getCurrentTime(); } catch (e) { return 0; } }
  function duration() { try { return av.getDuration(); } catch (e) { return 0; } }

  function stop() {
    if (cur) {
      var t = now();
      if (t > 0) localStorage.setItem('pos:' + cur.path, t);
    }
    try { av.stop(); av.close(); } catch (e) {}
    cur = null; seekTarget = null;
    hideOsd(); hidePill();
    show(open ? 'title' : 'browse');
    load();
  }

  function tick(ms) {
    if (cues) {
      var text = '';
      for (var i = 0; i < cues.length; i++) {
        if (cues[i].a <= ms && ms < cues[i].b) { text = cues[i].t; break; }
      }
      $('sub').textContent = text;
    }
    if (seekTarget === null && $('osd').className === 'show') draw(ms);
  }

  function fmt(ms) {
    var s = Math.floor(ms / 1000), h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60;
    s %= 60;
    return (h ? h + ':' : '') + (m < 10 && h ? '0' : '') + m + ':' + (s < 10 ? '0' : '') + s;
  }

  function draw(ms) {
    var d = duration(), pct = d ? Math.min(100, ms / d * 100) + '%' : '0';
    $('fill').style.width = pct;
    $('knob').style.left = pct;
    $('time').textContent = fmt(ms);
    $('left').textContent = d ? '-' + fmt(Math.max(0, d - ms)) : '';
  }

  // osd shows the seek bar; it hides after 4 s unless sticky or paused.
  function osd(sticky) {
    $('osd').className = 'show';
    $('sub').className = 'up';
    draw(seekTarget !== null ? seekTarget : now());
    clearTimeout(osdTimer);
    var paused = false;
    try { paused = av.getState() === 'PAUSED'; } catch (e) {}
    if (!sticky && !paused && seekTarget === null) osdTimer = setTimeout(hideOsd, 4000);
  }

  function hideOsd() { $('osd').className = ''; $('sub').className = ''; }

  // pill shows a short message at the top; it fades after 1.8 s unless sticky.
  function pill(msg, sticky) {
    $('pilltext').textContent = msg;
    $('pill').className = 'show';
    clearTimeout(pillTimer);
    if (!sticky) pillTimer = setTimeout(hidePill, 1800);
  }

  function hidePill() { $('pill').className = ''; }

  // scrub moves a preview position on the bar; OK seeks there, Back cancels.
  // Held keys speed up from 10 s to 30 s to 60 s steps.
  function scrub(dir, big) {
    var d = duration();
    if (!d) return;
    if (seekTarget === null) seekTarget = now();
    seekPresses = Date.now() - lastPress < 500 ? seekPresses + 1 : 1;
    lastPress = Date.now();
    var step = big || seekPresses > 10 ? 60000 : seekPresses > 4 ? 30000 : 10000;
    seekTarget = Math.max(0, Math.min(d - 2000, seekTarget + dir * step));
    var delta = seekTarget - now();
    pill((delta < 0 ? '−' : '+') + fmt(Math.abs(delta)) + '  →  ' + fmt(seekTarget) + '   ·   OK to jump', true);
    osd(true);
  }

  function cancelSeek() {
    seekTarget = null;
    hidePill();
    osd();
  }

  // commitSeek jumps to the scrubbed position and shows it in the pill. The
  // bar is drawn at the target since the player reports the new time late.
  function commitSeek(play) {
    var t = seekTarget;
    seekTarget = null;
    $('sub').textContent = '';
    try { av.seekTo(t); } catch (e) {}
    var paused = av.getState() === 'PAUSED';
    if (play && paused) { av.play(); paused = false; }
    pill((paused ? '❚❚  ' : '▶  ') + fmt(t), paused);
    osd(paused);
    draw(t);
  }

  function setSub(i) {
    subIdx = i;
    cues = null;
    $('sub').textContent = '';
    var s = subs[i];
    if (s && s.kind === 'embedded') try { av.setSelectTrack('TEXT', s.index); } catch (e) {}
    if (s && s.kind === 'file') get(base() + s.path, function (text) { if (subs[subIdx] === s) cues = parseSubs(text || ''); });
  }

  // cycleSub moves to the next subtitle. With none yet, it asks the server
  // again, since the server rips image subtitles to .srt after playback starts.
  function cycleSub() {
    if (subs.length < 2) {
      var v = cur;
      pill('Looking for subtitles…', true);
      return get(base() + '/api/list', function (text) {
        var found = [];
        try {
          JSON.parse(text).forEach(function (g) {
            g.videos.forEach(function (x) { if (x.path === v.path) found = x.subs; });
          });
        } catch (e) {}
        if (cur !== v) return;
        if (!found.length) return pill('No subtitles yet, try again in a moment');
        v.subs = found;
        found.forEach(function (p) { subs.push({ kind: 'file', path: p, label: decodeURIComponent(p.split('/').pop()) }); });
        setSub(1);
        pill('Subtitles: ' + subs[1].label);
      });
    }
    setSub((subIdx + 1) % subs.length);
    pill(subIdx ? 'Subtitles: ' + subs[subIdx].label + '  (' + subIdx + '/' + (subs.length - 1) + ')' : 'Subtitles off');
  }

  // parseSubs reads SRT or WebVTT cues into {a, b, t} with times in ms.
  function parseSubs(text) {
    var out = [], re = /(\d+):(\d\d):(\d\d)[,.](\d{3})\s*-->\s*(\d+):(\d\d):(\d\d)[,.](\d{3})[^\n]*\n([\s\S]*?)(?:\n\s*\n|$)/g, m;
    text = text.replace(/\r/g, '');
    var ms = function (h, mi, s, f) { return ((+h * 60 + +mi) * 60 + +s) * 1000 + +f; };
    while ((m = re.exec(text))) {
      out.push({ a: ms(m[1], m[2], m[3], m[4]), b: ms(m[5], m[6], m[7], m[8]), t: m[9].replace(/<[^>]+>|\{[^}]+\}/g, '').trim() });
    }
    return out;
  }

  function cycleAudio() {
    var a = tracks('AUDIO');
    if (a.length < 2) return pill(a.length ? 'Audio: ' + a[0].label + ' (only track)' : 'No audio tracks found');
    audioIdx = (audioIdx + 1) % a.length;
    try { av.setSelectTrack('AUDIO', a[audioIdx].index); } catch (e) { return pill('Audio switch failed'); }
    pill('Audio: ' + a[audioIdx].label + '  (' + (audioIdx + 1) + '/' + a.length + ')');
  }

  // toggle pauses or resumes. While scrubbing, it plays from the chosen spot.
  function toggle() {
    if (seekTarget !== null) return commitSeek(true);
    var st = av.getState();
    if (st === 'PLAYING') { av.pause(); pill('❚❚  ' + fmt(now()), true); osd(true); }
    else if (st === 'PAUSED' || st === 'READY') { av.play(); pill('▶  ' + fmt(now())); osd(); }
  }

  document.addEventListener('keydown', function (e) {
    var k = e.keyCode, screen = document.querySelector('.on').id;
    if (screen === 'setup') {
      // 65376 is "Done" on the Samsung on-screen keyboard.
      if (k === K.enter || k === 65376) {
        server = $('addr').value.trim();
        localStorage.setItem('server', server);
        $('addr').blur();
        load();
      } else if (k === K.back && lib.length) show('browse');
      return;
    }
    if (screen === 'browse') {
      var last = titles.length - 1;
      if (k === K.left) pick(tsel - 1);
      else if (k === K.right) pick(tsel + 1);
      else if (k === K.up) pick(tsel - COLS);
      else if (k === K.down) pick(Math.floor(tsel / COLS) < Math.floor(last / COLS) ? Math.min(tsel + COLS, last) : -1);
      else if (k === K.enter && titles[tsel]) openTitle(titles[tsel]);
      else if (k === K.ColorF1Green) { kind = (kind + 1) % KINDS.length; grid(); }
      else if (k === K.ColorF2Yellow) { sortBy = (sortBy + 1) % SORTS.length; grid(); }
      else if (k === K.ColorF0Red) setup();
      else if (k === K.back) tizen.application.getCurrentApplication().exit();
      return;
    }
    if (screen === 'title') {
      if (k === K.up) { move(-1); render(); }
      else if (k === K.down) { move(1); render(); }
      else if (k === K.enter && rows[sel] && !rows[sel].dir) play(rows[sel]);
      else if (k === K.back) { open = null; grid(); show('browse'); }
      return;
    }
    if (k === K.back && seekTarget !== null) cancelSeek();
    else if (k === K.back || k === K.MediaStop) stop();
    else if (k === K.enter || k === K.MediaPlayPause || k === K.MediaPlay || k === K.MediaPause) toggle();
    else if (k === K.left) scrub(-1);
    else if (k === K.right) scrub(1);
    else if (k === K.MediaRewind) scrub(-1, true);
    else if (k === K.MediaFastForward) scrub(1, true);
    else if (k === K.up) { cycleSub(); osd(); }
    else if (k === K.down) { cycleAudio(); osd(); }
    else osd();
  });

  document.addEventListener('visibilitychange', function () {
    if (document.hidden && cur) try { av.pause(); pill('❚❚  Paused', true); osd(true); } catch (e) {}
    if (!document.hidden && !$('player').classList.contains('on') && !$('setup').classList.contains('on')) load();
  });

  if (server) load(); else setup();
})();
