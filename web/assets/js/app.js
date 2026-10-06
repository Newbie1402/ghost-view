'use strict';

(() => {
  const $ = (selector) => document.querySelector(selector);
  const results = $('#results');
  const form = $('#search-form');
  const input = $('#search-input');
  const submit = $('#search-button');
  const dialog = $('#viewer');
  const platformNames = { tiktok: 'TikTok', instagram: 'Instagram', facebook: 'Facebook' };
  const mediaHosts = ['tiktok.com', 'tiktokcdn.com', 'tiktokcdn-us.com', 'instagram.com', 'cdninstagram.com', 'facebook.com', 'fbcdn.net'];
  const state = { platform: 'tiktok', providers: new Map(), controller: null, profile: null, posts: [], stories: [], highlights: [], resourceErrors: {}, cursor: '', tab: 'posts', viewItems: [], viewIndex: 0, paused: false, muted: true, story: false, elapsed: 0, lastFrame: 0, frame: 0, opener: null };

  function node(tag, className, text) {
    const item = document.createElement(tag);
    if (className) item.className = className;
    if (text !== undefined && text !== null) item.textContent = String(text);
    return item;
  }
  function icon(name) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
    svg.setAttribute('aria-hidden', 'true');
    use.setAttribute('href', `/assets/icons/sprite.svg#${name}`);
    svg.append(use);
    return svg;
  }
  function safeURL(value) {
    if (typeof value !== 'string' || !value || value.includes('\\')) return '';
    try {
      const url = new URL(value, location.origin);
      if (url.username || url.password) return '';
      if (url.origin === location.origin && url.pathname.startsWith('/assets/fixtures/')) return url.href;
      if (url.protocol !== 'https:' || (url.port && url.port !== '443')) return '';
      if (mediaHosts.some(host => url.hostname === host || url.hostname.endsWith(`.${host}`))) return url.href;
    } catch (_) { /* Invalid provider URL remains unavailable. */ }
    return '';
  }
  function image(url, alt, className = '') {
    const img = node('img', className);
    img.alt = alt;
    img.loading = 'lazy';
    img.decoding = 'async';
    img.referrerPolicy = 'no-referrer';
    const safe = safeURL(url);
    if (safe) img.src = safe;
    img.addEventListener('error', () => { img.removeAttribute('src'); img.alt = 'Image unavailable'; }, { once: true });
    return img;
  }
  function button(text, className, action) {
    const item = node('button', className, text);
    item.type = 'button';
    item.addEventListener('click', action);
    return item;
  }
  function downloadLink(media, className = 'download-button') {
    const link = node('a', className);
    link.href = `/api/v1/media/${encodeURIComponent(state.platform)}/${encodeURIComponent(media.id)}/download`;
    link.setAttribute('download', '');
    link.setAttribute('aria-label', `Download ${media.type.toLowerCase()}`);
    link.title = 'Download media';
    link.append(icon('download'));
    return link;
  }
  async function api(path, signal = state.controller?.signal) {
    const response = await fetch(`/api/v1${path}`, { signal, headers: { Accept: 'application/json' }, credentials: 'omit' });
    let payload;
    try { payload = await response.json(); } catch (_) { throw new Error('The service did not return a valid response. Please try again.'); }
    if (!response.ok || payload.error) {
      const error = new Error(payload.error?.message || 'The request could not be completed. Please try again.');
      error.code = payload.error?.code;
      throw error;
    }
    return payload.data;
  }
  function updateSource() {
    const provider = state.providers.get(state.platform);
    $('#data-source').textContent = `DATA SOURCE: ${provider?.dataSource || 'UNKNOWN'}`;
    $('#mode-badge').textContent = provider?.status || 'Unavailable';
    $('#mode-copy').textContent = provider?.message || 'Provider information is unavailable. No fixture fallback is used.';
  }
  function selectPlatform(platform) {
    if (!Object.hasOwn(platformNames, platform)) return;
    state.platform = platform;
    document.querySelectorAll('[data-platform]').forEach(item => {
      const active = item.dataset.platform === platform;
      item.classList.toggle('active', active);
      item.setAttribute('aria-pressed', String(active));
    });
    updateSource();
  }
  function beginRequest() {
    state.controller?.abort();
    state.controller = new AbortController();
    if (dialog.open) dialog.close();
    results.hidden = false;
    $('#intro').hidden = true;
    $('.search-section').classList.add('compact');
    results.setAttribute('aria-busy', 'true');
    submit.setAttribute('aria-busy', 'true');
    submit.firstChild.textContent = 'Searching ';
    skeleton();
    return state.controller.signal;
  }
  function endRequest(signal) {
    if (signal.aborted) return;
    results.setAttribute('aria-busy', 'false');
    submit.disabled = false;
    submit.setAttribute('aria-busy', 'false');
    submit.firstChild.textContent = 'Search ';
  }
  function skeleton() {
    results.replaceChildren();
    const wrap = node('div', 'skeleton-profile');
    wrap.setAttribute('aria-label', 'Loading profile');
    wrap.append(node('div', 'skeleton'));
    const lines = node('div', 'skeleton-lines');
    for (let i = 0; i < 3; i++) lines.append(node('div', 'skeleton'));
    wrap.append(lines);
    const grid = node('div', 'skeleton-grid');
    for (let i = 0; i < 4; i++) grid.append(node('div', 'skeleton'));
    results.append(wrap, grid);
  }
  function resetSearch() {
    state.controller?.abort();
    results.hidden = true;
    results.replaceChildren();
    $('#intro').hidden = false;
    $('.search-section').classList.remove('compact');
    submit.disabled = false;
    submit.setAttribute('aria-busy', 'false');
    submit.firstChild.textContent = 'Search ';
    results.setAttribute('aria-busy', 'false');
    input.focus();
  }
  function heading(text) {
    const wrap = node('div', 'result-heading');
    wrap.append(node('h2', '', text), button('Back to search', 'text-button', resetSearch));
    return wrap;
  }
  function showState(title, message, symbol = 'search') {
    results.replaceChildren();
    const panel = node('div', 'state-panel');
    panel.append(icon(symbol), node('h2', '', title), node('p', '', message), button('Try another search', 'primary-button', resetSearch));
    results.append(panel);
  }
  function showError(error) {
    if (error.name === 'AbortError') return;
    if (error.code === 'PRIVATE' || error.code === 'PROFILE_PRIVATE') {
      showState('This profile is private.', 'GhostView cannot access content restricted by the platform.', 'lock');
      return;
    }
    const unavailable = ['UNAVAILABLE', 'PROVIDER_UNAVAILABLE', 'UNSUPPORTED_CAPABILITY', 'UNSUPPORTED'].includes(error.code);
    showState(unavailable ? 'This content is unavailable.' : 'We couldn’t complete that search.', error.message, unavailable ? 'lock' : 'search');
  }
  async function search(query) {
    input.value = query.trim();
    if (!input.value) { input.focus(); return; }
    const signal = beginRequest();
    try {
      const data = await api(`/search?platform=${encodeURIComponent(state.platform)}&q=${encodeURIComponent(input.value)}`, signal);
      if (signal.aborted) return;
      selectPlatform(data.platform);
      const matches = data.profiles || [];
      if (matches.length === 1) await openProfile(matches[0].username, signal);
      else if (matches.length > 1) renderCandidates(matches);
      else showState('No profiles found.', state.providers.get(state.platform)?.dataSource === 'MOCK' ? 'Try @alex.morgan in fixture mode.' : 'Try an exact public username or a supported profile URL. Display-name discovery is unavailable for this provider.');
    } catch (error) { if (!signal.aborted) showError(error); }
    finally { endRequest(signal); }
  }
  function renderCandidates(profiles) {
    results.replaceChildren(heading('Choose a profile'));
    const list = node('div', 'candidate-list');
    profiles.forEach(profile => {
      const item = button('', 'candidate', async () => {
        selectPlatform(profile.platform);
        const signal = beginRequest();
        try { await openProfile(profile.username, signal); }
        catch (error) { if (!signal.aborted) showError(error); }
        finally { endRequest(signal); }
      });
      item.append(image(profile.avatarURL, '', 'avatar'));
      const copy = node('div', 'candidate-copy');
      const name = node('strong', '', profile.displayName || profile.username);
      if (profile.verified) name.append(icon('verified'));
      copy.append(name, node('small', '', `@${profile.username}`));
      item.append(copy, node('span', 'badge', platformNames[profile.platform] || profile.platform), icon('arrow'));
      list.append(item);
    });
    results.append(list);
  }
  async function openProfile(username, signal) {
    const path = `/profiles/${encodeURIComponent(state.platform)}/${encodeURIComponent(username)}`;
    const profile = await api(path, signal);
    if (signal.aborted) return;
    state.profile = profile;
    if (profile.accessStatus !== 'PUBLIC') {
      showState(profile.accessStatus === 'PRIVATE' ? 'This profile is private.' : 'This profile is unavailable.', profile.accessStatus === 'PRIVATE' ? 'GhostView cannot access content restricted by the platform.' : (profile.message || 'The provider cannot retrieve publicly accessible information for this profile.'), 'lock');
      return;
    }
    await providerReady;
    if (signal.aborted) return;
    const caps = state.providers.get(state.platform)?.capabilities || profile.capabilities || {};
    const requests = ['posts', 'stories', 'highlights'].map(resource => caps[resource] ? api(`${path}/${resource}`, signal) : Promise.resolve(null));
    const media = await Promise.allSettled(requests);
    if (signal.aborted) return;
    state.resourceErrors = {};
    ['posts', 'stories', 'highlights'].forEach((resource, index) => {
      if (media[index].status === 'rejected') state.resourceErrors[resource] = media[index].reason;
    });
    state.posts = media[0].status === 'fulfilled' ? media[0].value?.items || [] : [];
    state.cursor = media[0].status === 'fulfilled' ? media[0].value?.nextCursor || '' : '';
    state.stories = media[1].status === 'fulfilled' ? media[1].value || [] : [];
    state.highlights = media[2].status === 'fulfilled' ? media[2].value || [] : [];
    state.tab = caps.posts ? 'posts' : caps.stories ? 'stories' : caps.highlights ? 'highlights' : '';
    renderProfile(caps);
  }
  function renderProfile(caps) {
    const profile = state.profile;
    results.replaceChildren(heading('Public profile'));
    const header = node('div', 'profile-heading');
    header.append(image(profile.avatarURL, `${profile.displayName || profile.username}'s avatar`, 'avatar'));
    const copy = node('div', 'profile-copy');
    const name = node('div', 'profile-name');
    name.append(node('h2', '', profile.displayName || profile.username));
    if (profile.verified) { const verified = icon('verified'); verified.setAttribute('aria-label', 'Verified profile'); verified.removeAttribute('aria-hidden'); name.append(verified); }
    copy.append(name, node('p', 'profile-username', `@${profile.username} · ${platformNames[state.platform]}`));
    if (profile.bio) copy.append(node('p', 'profile-bio', profile.bio));
    const stats = node('div', 'profile-stats');
    [['followerCount', 'Followers'], ['followingCount', 'Following'], ['postCount', 'Posts']].forEach(([key, label]) => {
      if (profile[key] !== null && profile[key] !== undefined) {
        const stat = node('p');
        stat.append(node('strong', '', Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(profile[key])), document.createTextNode(label));
        stats.append(stat);
      }
    });
    if (stats.childElementCount) copy.append(stats);
    else copy.append(node('p', 'profile-username', 'Profile statistics are unavailable.'));
    header.append(copy, node('span', 'badge profile-tag', `DATA SOURCE: ${profile.dataSource || 'UNKNOWN'}`));
    results.append(header);
    const tabs = node('div', 'tabs');
    tabs.setAttribute('role', 'tablist');
    tabs.setAttribute('aria-label', 'Profile content');
    const options = [];
    if (caps.stories) options.push(['stories', 'Stories']);
    if (caps.posts) options.push(['posts', 'Posts']);
    if (caps.posts && state.posts.some(media => media.type === 'REEL')) options.push(['reels', 'Reels']);
    if (caps.highlights) options.push(['highlights', 'Highlights']);
    options.forEach(([key, label]) => {
      const tab = button(label, 'tab', () => selectTab(key));
      tab.id = `tab-${key}`;
      tab.dataset.tab = key;
      tab.setAttribute('role', 'tab');
      tab.setAttribute('aria-controls', 'media-panel');
      tab.addEventListener('keydown', event => {
        if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
        event.preventDefault();
        const index = options.findIndex(option => option[0] === state.tab);
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + options.length) % options.length;
        selectTab(options[next][0]);
        $(`#tab-${options[next][0]}`).focus();
      });
      tabs.append(tab);
    });
    const content = node('div', 'media-section');
    content.id = 'media-panel';
    content.setAttribute('role', 'tabpanel');
    if (options.length) results.append(tabs, content);
    else { content.append(node('p', 'empty-media', 'This provider supplies profile information only. Media is unavailable.')); results.append(content); }
    selectTab(state.tab);
  }
  function selectTab(key) {
    state.tab = key;
    document.querySelectorAll('[data-tab]').forEach(tab => {
      const active = tab.dataset.tab === key;
      tab.classList.toggle('active', active);
      tab.setAttribute('aria-selected', String(active));
      tab.tabIndex = active ? 0 : -1;
    });
    const panel = $('#media-panel');
    panel.setAttribute('aria-labelledby', `tab-${key}`);
    if (!key) return;
    panel.replaceChildren();
    const resource = key === 'reels' ? 'posts' : key;
    if (state.resourceErrors[resource]) {
      const wrap = node('div', 'inline-error', state.resourceErrors[resource].message);
      wrap.append(button('Try again', '', () => search(`@${state.profile.username}`)));
      panel.append(wrap);
      return;
    }
    if (key === 'highlights') {
      if (!state.highlights.length) { panel.append(node('p', 'empty-media', 'No publicly available highlights were supplied by this provider.')); return; }
      const grid = node('div', 'media-grid stories-grid');
      state.highlights.forEach(highlight => {
        const card = node('article', 'media-card');
        const view = button('', 'media-cover', () => openViewer(highlight.items || [], 0, true, highlight.title));
        view.setAttribute('aria-label', `View highlight: ${highlight.title}`);
        view.append(image(highlight.thumbnailURL, highlight.title));
        card.append(view, node('span', 'highlight-title', highlight.title));
        grid.append(card);
      });
      panel.append(grid);
      return;
    }
    const items = key === 'stories' ? state.stories : key === 'reels' ? state.posts.filter(item => item.type === 'REEL') : state.posts;
    if (!items.length) panel.append(node('p', 'empty-media', `No publicly available ${key} were supplied by this provider.`));
    else {
      const grid = node('div', `media-grid${key === 'stories' ? ' stories-grid' : ''}`);
      items.forEach((media, index) => {
        const card = node('article', 'media-card');
        const cover = button('', 'media-cover', () => openViewer(items, index, key === 'stories'));
        cover.setAttribute('aria-label', `View ${media.type.toLowerCase()}: ${media.caption || `media ${index + 1}`}`);
        cover.append(image(media.thumbnailURL || media.mediaURL, media.caption || `${media.type.toLowerCase()} thumbnail`));
        const type = node('span', 'media-type', media.previewOnly ? 'Image preview' : media.type.charAt(0) + media.type.slice(1).toLowerCase());
        type.prepend(icon(isVideo(media) ? 'play' : 'image'));
        cover.append(type);
        if (media.duration) cover.append(node('span', 'media-duration', duration(media.duration)));
        const actions = node('div', 'media-actions');
        actions.append(node('p', 'media-caption', media.caption || 'Public media'));
        if (media.downloadable && state.providers.get(state.platform)?.capabilities.downloads) actions.append(downloadLink(media));
        card.append(cover, actions);
        grid.append(card);
      });
      panel.append(grid);
    }
    if (state.cursor && (key === 'posts' || key === 'reels')) panel.append(button('Load more', 'load-more', loadMore));
  }
  async function loadMore(event) {
    const load = event.currentTarget;
    const signal = state.controller.signal;
    load.disabled = true;
    load.textContent = 'Loading…';
    try {
      const data = await api(`/profiles/${encodeURIComponent(state.platform)}/${encodeURIComponent(state.profile.username)}/posts?cursor=${encodeURIComponent(state.cursor)}`, signal);
      if (signal.aborted) return;
      state.posts.push(...(data.items || []));
      state.cursor = data.nextCursor || '';
      renderProfile(state.providers.get(state.platform)?.capabilities || {});
    } catch (error) {
      if (!signal.aborted) { load.textContent = 'Retry loading'; load.disabled = false; const message = node('p', 'inline-error', error.message); load.after(message); }
    }
  }
  function duration(seconds) {
    return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
  }
  function isVideo(media) {
    return media.type === 'VIDEO' || media.type === 'REEL' || /\.(mp4|webm|mov)(?:\?|$)/i.test(media.mediaURL || '');
  }
  function openViewer(items, index, story = false, title = 'Public media') {
    if (!items.length) return;
    state.viewItems = items;
    state.viewIndex = index;
    state.story = story;
    state.paused = !story;
    state.opener = document.activeElement;
    $('#viewer-title').textContent = title;
    document.body.classList.add('viewer-open');
    dialog.showModal();
    renderViewer();
  }
  function setControl(selector, symbol, text) {
    const control = $(selector);
    control.replaceChildren(icon(symbol), node('span', '', text));
    control.setAttribute('aria-label', text);
  }
  function renderViewer() {
    cancelAnimationFrame(state.frame);
    const previousVideo = $('#viewer-media video');
    previousVideo?.pause();
    state.elapsed = 0;
    state.lastFrame = 0;
    const media = state.viewItems[state.viewIndex];
    const area = $('#viewer-media');
    area.replaceChildren();
    const url = safeURL(media.mediaURL);
    $('#viewer-position').textContent = `${state.viewIndex + 1} of ${state.viewItems.length}`;
    $('#viewer-caption').textContent = media.caption || '';
    $('#viewer-prev').disabled = state.viewIndex === 0;
    $('#viewer-next').disabled = state.viewIndex === state.viewItems.length - 1;
    $('#viewer-error').hidden = true;
    $('#viewer-progress').hidden = !state.story;
    $('#viewer-progress span').style.transform = 'scaleX(0)';
    $('#viewer-mute').hidden = !isVideo(media);
    $('#viewer-play').hidden = !state.story && !isVideo(media);
    const download = $('#viewer-download');
    download.hidden = !media.downloadable || !state.providers.get(state.platform)?.capabilities.downloads;
    download.href = `/api/v1/media/${encodeURIComponent(state.platform)}/${encodeURIComponent(media.id)}/download`;
    download.setAttribute('download', '');
    if (!url) { area.append(node('p', 'empty-media', 'This media is unavailable.')); state.paused = true; }
    else if (isVideo(media)) {
      const video = node('video');
      video.src = url;
      video.playsInline = true;
      video.muted = state.muted;
      video.controls = true;
      video.preload = 'metadata';
      video.setAttribute('aria-label', media.caption || 'Public video');
      const poster = safeURL(media.thumbnailURL);
      if (poster) video.poster = poster;
      video.addEventListener('error', () => { viewerError('This video is unavailable. You can move to the next item.'); state.paused = true; updateControls(); });
      video.addEventListener('ended', () => { if (state.story && !state.paused) advanceViewer(1); else { state.paused = true; updateControls(); } });
      video.addEventListener('play', () => { state.paused = false; updateControls(); });
      video.addEventListener('pause', () => { if (video === $('#viewer-media video') && !video.ended) { state.paused = true; updateControls(); } });
      video.addEventListener('volumechange', () => { state.muted = video.muted; updateControls(); });
      video.addEventListener('timeupdate', () => { if (state.story && video.duration) $('#viewer-progress span').style.transform = `scaleX(${Math.min(1, video.currentTime / video.duration)})`; });
      area.append(video);
      if (!state.paused) video.play().catch(() => { state.paused = true; updateControls(); });
    } else {
      const img = image(url, media.caption || 'Public image');
      img.loading = 'eager';
      img.addEventListener('error', () => { state.paused = true; updateControls(); });
      area.append(img);
      if (state.story) {
        const start = () => { if (dialog.open && img === $('#viewer-media img')) state.frame = requestAnimationFrame(storyFrame); };
        if (img.complete) start(); else img.addEventListener('load', start, { once: true });
      }
    }
    updateControls();
  }
  function viewerError(message) { $('#viewer-error').textContent = message; $('#viewer-error').hidden = false; }
  function updateControls() {
    setControl('#viewer-play', state.paused ? 'play' : 'pause', state.paused ? 'Play' : 'Pause');
    setControl('#viewer-mute', state.muted ? 'mute' : 'volume', state.muted ? 'Unmute' : 'Mute');
  }
  function storyFrame(now) {
    if (!dialog.open) return;
    if (state.lastFrame && !state.paused && !document.hidden) state.elapsed += Math.min(now - state.lastFrame, 250);
    state.lastFrame = now;
    $('#viewer-progress span').style.transform = `scaleX(${Math.min(state.elapsed / 5000, 1)})`;
    if (state.elapsed >= 5000) { advanceViewer(1); return; }
    state.frame = requestAnimationFrame(storyFrame);
  }
  function advanceViewer(delta) {
    const next = state.viewIndex + delta;
    if (next < 0) return;
    if (next >= state.viewItems.length) {
      state.paused = true;
      state.elapsed = 0;
      state.lastFrame = 0;
      updateControls();
      if (state.story && !isVideo(state.viewItems[state.viewIndex])) state.frame = requestAnimationFrame(storyFrame);
      return;
    }
    state.viewIndex = next;
    renderViewer();
  }
  $('#viewer-close').addEventListener('click', () => dialog.close());
  $('#viewer-prev').addEventListener('click', () => advanceViewer(-1));
  $('#viewer-next').addEventListener('click', () => advanceViewer(1));
  $('#viewer-play').addEventListener('click', () => {
    state.paused = !state.paused;
    const video = $('#viewer-media video');
    if (video) {
      if (state.paused) video.pause();
      else video.play().catch(() => { state.paused = true; viewerError('Playback could not start. Use the video controls to try again.'); updateControls(); });
    }
    updateControls();
  });
  $('#viewer-mute').addEventListener('click', () => {
    state.muted = !state.muted;
    const video = $('#viewer-media video');
    if (video) video.muted = state.muted;
    updateControls();
  });
  dialog.addEventListener('close', () => {
    cancelAnimationFrame(state.frame);
    $('#viewer-media video')?.pause();
    $('#viewer-media').replaceChildren();
    document.body.classList.remove('viewer-open');
    if (state.opener?.isConnected) state.opener.focus();
  });
  dialog.addEventListener('keydown', event => {
    if (event.target instanceof HTMLVideoElement) return;
    if (event.key === 'ArrowLeft') { event.preventDefault(); advanceViewer(-1); }
    if (event.key === 'ArrowRight') { event.preventDefault(); advanceViewer(1); }
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden && dialog.open) {
      state.paused = true;
      $('#viewer-media video')?.pause();
      updateControls();
    }
  });
  form.addEventListener('submit', event => { event.preventDefault(); search(input.value); });
  document.querySelectorAll('[data-platform]').forEach(item => item.addEventListener('click', () => { resetSearch(); selectPlatform(item.dataset.platform); input.focus(); }));
  document.querySelectorAll('[data-query]').forEach(item => item.addEventListener('click', () => search(item.dataset.query)));
  document.querySelectorAll('[data-info]').forEach(item => item.addEventListener('click', () => {
    const info = $('#footer-info');
    const same = info.dataset.current === item.dataset.info;
    info.hidden = same && !info.hidden;
    info.dataset.current = item.dataset.info;
    info.textContent = item.dataset.info === 'privacy' ? 'GhostView displays only information supplied by its configured public-content provider. It cannot access private accounts, bypass restrictions or login walls. Never provide platform passwords, cookies or session details. Searches are sent to this application’s server; media requests may contact the content host. Viewing media here does not deliberately send platform view-registration events.' : 'Demo mode uses deterministic, fictional local profiles and media. @alex.morgan is public, “alex” returns multiple profiles, and @private.user demonstrates protected content. Demo data does not come from TikTok, Instagram or Facebook. REAL data comes from configured public integrations. Only listed capabilities are supported. Unavailable retrieval never falls back to fixtures.';
    item.setAttribute('aria-expanded', String(!info.hidden));
  }));
  function applyTheme(theme) {
    document.documentElement.dataset.theme = theme;
    $('#theme-toggle').setAttribute('aria-label', theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme');
    $('#theme-toggle').replaceChildren(icon(theme === 'dark' ? 'moon' : 'sun'));
  }
  try { applyTheme(localStorage.getItem('ghostview-theme') === 'dark' ? 'dark' : 'light'); } catch (_) { applyTheme('light'); }
  $('#theme-toggle').addEventListener('click', () => {
    const theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    applyTheme(theme);
    try { localStorage.setItem('ghostview-theme', theme); } catch (_) { /* Theme works without storage access. */ }
  });
  const providerReady = api('/providers', null).then(providers => {
    providers.forEach(provider => {
      state.providers.set(provider.platform, provider);
      const status = $(`[data-status="${provider.platform}"]`);
      const copy = $(`[data-capability="${provider.platform}"]`);
      if (status) status.textContent = provider.status;
      const supported = ['profile', 'posts', 'stories', 'highlights', 'downloads'].filter(key => provider.capabilities[key]);
      if (copy) copy.textContent = supported.length ? `${supported.map(key => key === 'profile' ? 'Profiles' : key).join(', ')}${provider.status === 'MOCK' ? ' in the local demo.' : '.'}` : 'Live public-data integration is unavailable.';
    });
    const mock = providers.every(provider => provider.status === 'MOCK');
    updateSource();
    $('#demo-examples').hidden = !mock;
  }).catch(() => {
    $('#data-source').textContent = 'DATA SOURCE: UNKNOWN';
    $('#mode-badge').textContent = 'Unavailable';
    $('#mode-copy').textContent = 'Provider information could not load. Please refresh to try again.';
    document.querySelectorAll('[data-status]').forEach(item => { item.textContent = 'UNAVAILABLE'; });
    document.querySelectorAll('[data-capability]').forEach(item => { item.textContent = 'Provider capability information is unavailable.'; });
    $('#demo-examples').hidden = true;
  });
})();
