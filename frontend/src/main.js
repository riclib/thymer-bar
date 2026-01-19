// thymer-bar — Dashboard-first Command Center
// Dashboard (primary) → Settings (cogwheel) → back to Dashboard

import {
    // Plugin Manager
    GetPluginManagerHTML,
    GetPluginInfoHTML,
    GetDefaultInfoHTML,
    InstallPlugin,
    UpdatePlugin,
    UpdateAllPlugins,
    SetDevMode,
    HideWindow,
    // Sync Manager
    GetSyncManagerHTML,
    GetSyncSourceInfoHTML,
    GetSyncDefaultInfoHTML,
    ConnectSource,
    DisconnectSource,
    SetSourceEnabled,
    SyncNow,
    SyncAll,
    ResyncAll,
    ResyncSource,
    RerenderAll,
    RerenderSource,
    GetGitHubConfigHTML,
    SetGitHubRepos,
    SetGitHubToken,
    SetGoogleCredentials,
    HasGoogleCredentials,
    GetGoogleCalendarConfigHTML,
    SetGoogleCalendars,
    // Dashboard
    GetDashboardHTML,
    GetDashboardData,
    GetTodayTasks,
    GetCalendarEvents,
    GetConnectionStatus,
    StartSession,
    PauseSession,
    ResumeSession,
    CompleteSession,
    ToggleHabit,
    NavigateToRecord
} from '../wailsjs/go/main/App';

import { EventsOn } from '../wailsjs/runtime/runtime';

const app = document.getElementById('app');
let currentView = 'dashboard'; // 'dashboard' | 'settings'
let settingsTab = 'sync'; // 'plugins' | 'sync'
let selectedPluginId = null;
let selectedSourceId = null;
let wasConnected = false; // Track connection state for refresh on reconnect

// Session state (synced with backend on dashboard render)
let sessionState = {
    active: false,
    paused: false,
    taskGuid: null,
    taskTitle: '',
    startTime: null,
    elapsed: 0
};
let timerInterval = null;
let nowLineInterval = null;

// ============================================================================
// Event Listeners
// ============================================================================

EventsOn('thymer:connection', (data) => {
    updateConnectionStatus(data.connected, data.count);

    // Refresh dashboard when connection is established (initial or reconnect)
    if (data.connected && !wasConnected) {
        console.log('[thymer-bar] Connected to Thymer, refreshing dashboard');
        if (currentView === 'dashboard') {
            renderDashboard();
        }
    }
    wasConnected = data.connected;
});

EventsOn('sync:progress', (data) => {
    updateSyncProgress(data);
});

EventsOn('session:update', (data) => {
    sessionState = { ...sessionState, ...data };
    if (currentView === 'dashboard') {
        updateFocusPanel();
    }
});

EventsOn('navigate', async (target) => {
    if (target === 'settings' && currentView !== 'settings') {
        currentView = 'settings';
        await init();
    } else if (target === 'dashboard' && currentView !== 'dashboard') {
        currentView = 'dashboard';
        await init();
    }
});

EventsOn('dailynote:changed', async (data) => {
    console.log('[thymer-bar] Daily note changed:', data);
    if (currentView === 'dashboard') {
        await renderDashboard();
    }
});

// ============================================================================
// Initialization
// ============================================================================

async function init() {
    try {
        // Check initial connection status
        const status = await GetConnectionStatus().catch(() => ({ connected: false }));
        wasConnected = status.connected;

        if (currentView === 'dashboard') {
            await renderDashboard();
        } else {
            await renderSettings();
        }
        bindEvents();
    } catch (err) {
        console.error('Failed to load:', err);
        app.innerHTML = `
            <div class="db-empty">
                <p>Failed to load</p>
                <p>${err}</p>
            </div>
        `;
    }
}

// ============================================================================
// Dashboard View
// ============================================================================

async function renderDashboard() {
    try {
        // Get dashboard data to sync session state
        const data = await GetDashboardData();

        // Sync frontend session state with backend
        if (data.session && (data.session.active || data.session.paused)) {
            sessionState = {
                active: data.session.active,
                paused: data.session.paused,
                taskGuid: data.session.taskGUID,
                taskTitle: data.session.taskTitle,
                startTime: Date.now() - (data.session.elapsed * 1000),
                elapsed: data.session.elapsed
            };
            // Start timer if active (not paused)
            if (data.session.active && !timerInterval) {
                startTimerTick();
            } else if (data.session.paused) {
                stopTimerTick();
            }
        } else {
            // No active session
            sessionState = {
                active: false,
                paused: false,
                taskGuid: null,
                taskTitle: '',
                startTime: null,
                elapsed: 0
            };
            stopTimerTick();
        }

        // Get pre-rendered HTML from Go/templ
        const html = await GetDashboardHTML();
        app.innerHTML = html;

        // Start now line updates and auto-scroll
        startNowLineUpdates();

        // Auto-scroll timeline to now
        scrollTimelineToNow();
    } catch (err) {
        console.error('Failed to render dashboard:', err);
        app.innerHTML = `
            <div class="db-empty">
                <p>Failed to load dashboard</p>
                <p>${err}</p>
            </div>
        `;
    }
}

function updateFocusPanel() {
    // Re-render the whole dashboard for now
    // Could optimize later to just update the focus panel via a separate API
    renderDashboard();
}

// ============================================================================
// Settings View
// ============================================================================

async function renderSettings() {
    const contentHtml = settingsTab === 'sync'
        ? await GetSyncManagerHTML()
        : await GetPluginManagerHTML();

    app.innerHTML = `
        <div class="settings-view">
            <div class="settings-header">
                <button class="settings-back" data-action="back-to-dashboard">
                    <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>
                    Dashboard
                </button>
                <div class="settings-tabs">
                    <button class="settings-tab ${settingsTab === 'sync' ? 'active' : ''}" data-action="switch-settings-tab" data-tab="sync">
                        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 21h5v-5"/></svg>
                        Sync
                    </button>
                    <button class="settings-tab ${settingsTab === 'plugins' ? 'active' : ''}" data-action="switch-settings-tab" data-tab="plugins">
                        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
                        Plugins
                    </button>
                </div>
            </div>
            <div class="settings-content">
                ${contentHtml}
            </div>
        </div>
    `;

    selectedPluginId = null;
    selectedSourceId = null;
}

// ============================================================================
// Timer
// ============================================================================

function startTimerTick() {
    if (timerInterval) clearInterval(timerInterval);
    timerInterval = setInterval(() => {
        if (sessionState.active) {
            sessionState.elapsed++;
            // Update focus panel timer
            const timerEl = document.getElementById('timer');
            if (timerEl) {
                timerEl.textContent = formatTime(sessionState.elapsed);
            }
            // Update progress ring
            const progress = Math.min(sessionState.elapsed / 7200, 1);
            const circumference = 942;
            const offset = circumference - (progress * circumference);
            const ring = document.querySelector('.db-ring-progress');
            if (ring) {
                ring.style.strokeDashoffset = offset;
            }
            // Update active task card's elapsed time
            if (sessionState.taskGuid) {
                const card = document.querySelector(`.db-q-item[data-guid="${sessionState.taskGuid}"]`);
                if (card) {
                    let elapsedEl = card.querySelector('.db-q-elapsed');
                    if (!elapsedEl) {
                        // Create elapsed element if it doesn't exist
                        const emptyEl = card.querySelector('.db-q-elapsed-empty, .db-q-estimate');
                        if (emptyEl) {
                            elapsedEl = document.createElement('span');
                            elapsedEl.className = 'db-q-elapsed';
                            emptyEl.replaceWith(elapsedEl);
                        }
                    }
                    if (elapsedEl) {
                        elapsedEl.textContent = formatDurationShort(sessionState.elapsed);
                    }
                }
            }
        }
    }, 1000);
}

function stopTimerTick() {
    if (timerInterval) {
        clearInterval(timerInterval);
        timerInterval = null;
    }
}

// ============================================================================
// Now Line Updates & Timeline Scrolling
// ============================================================================

function startNowLineUpdates() {
    // Clear any existing interval
    if (nowLineInterval) {
        clearInterval(nowLineInterval);
    }

    // Update now line position immediately, then every minute
    updateNowLinePosition();
    nowLineInterval = setInterval(() => {
        updateNowLinePosition();
        scrollTimelineToNow(true); // Smooth scroll as time passes
    }, 60000);
}

function updateNowLinePosition() {
    const nowLine = document.querySelector('.db-now-line');
    if (!nowLine) return;

    const now = new Date();
    const hour = now.getHours();
    const minute = now.getMinutes();

    // Timeline spans 24 hours (1440 minutes)
    const minutesFromStart = hour * 60 + minute;
    const percent = (minutesFromStart / 1440) * 100;
    nowLine.style.top = `${percent.toFixed(2)}%`;
}

function scrollTimelineToNow(smooth = false) {
    const timeline = document.querySelector('.db-timeline-body');
    if (!timeline) return;

    const now = new Date();
    const hour = now.getHours();
    const minute = now.getMinutes();

    // Calculate pixel position (each hour = 60px)
    const pixelPosition = (hour * 60) + minute;

    // Center in viewport
    const viewportHeight = timeline.clientHeight;
    const scrollTarget = Math.max(0, pixelPosition - viewportHeight / 2);

    timeline.scrollTo({
        top: scrollTarget,
        behavior: smooth ? 'smooth' : 'instant'
    });
}

// ============================================================================
// Utilities
// ============================================================================

function formatTime(secs) {
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    const s = secs % 60;
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
}

function formatDurationShort(secs) {
    if (secs < 60) return `${secs}s`;
    const m = Math.floor(secs / 60);
    if (m < 60) return `${m}m`;
    const h = Math.floor(m / 60);
    const remainingM = m % 60;
    return remainingM > 0 ? `${h}h${remainingM}m` : `${h}h`;
}

function formatTimeShort(dateStr) {
    const d = new Date(dateStr);
    const h = d.getHours();
    const m = d.getMinutes();
    const ampm = h >= 12 ? 'p' : 'a';
    const hour = h % 12 || 12;
    return m === 0 ? `${hour}${ampm}` : `${hour}:${m.toString().padStart(2, '0')}${ampm}`;
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateConnectionStatus(connected, count) {
    // Update settings view status dot
    const statusDot = document.querySelector('.pm-status-dot, .sm-card-indicator');
    if (statusDot) {
        statusDot.classList.toggle('connected', connected);
    }

    // Update dashboard connection badge
    const dbConnection = document.querySelector('.db-connection');
    if (dbConnection) {
        dbConnection.classList.toggle('connected', connected);
        const text = dbConnection.querySelector('.db-connection-text');
        if (text) {
            text.textContent = connected ? 'Connected' : 'Offline';
        }
        dbConnection.title = connected ? 'Connected to Thymer' : 'Thymer not connected';
    }
}

function updateSyncProgress(progress) {
    const { source, phase, processed, created, updated, unchanged, errors } = progress;
    const card = document.querySelector(`[data-source="${source}"]`);
    if (!card) return;

    const btn = card.querySelector('[data-action="sync-now"]');
    if (!btn) return;

    const span = btn.querySelector('span');
    if (span) {
        if (phase === 'complete') {
            span.textContent = 'Sync Now';
        } else if (phase === 'error') {
            span.textContent = 'Error';
        } else {
            span.textContent = `${phase}... ${processed}`;
        }
    }
}

function showToast(message, type = 'info') {
    const existing = document.querySelector('.pm-toast');
    if (existing) existing.remove();

    const toast = document.createElement('div');
    toast.className = `pm-toast ${type}`;
    toast.innerHTML = `
        ${type === 'success' ? '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><path d="m9 11 3 3L22 4"/></svg>' : ''}
        ${type === 'error' ? '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>' : ''}
        <span>${message}</span>
    `;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
}

// ============================================================================
// Event Binding
// ============================================================================

let eventsBound = false;

function bindEvents() {
    if (eventsBound) return;
    eventsBound = true;

    app.addEventListener('click', async (e) => {
        const target = e.target.closest('[data-action]');
        if (!target) return;

        const action = target.dataset.action;

        switch (action) {
            // Navigation
            case 'open-settings':
                currentView = 'settings';
                await init();
                break;
            case 'back-to-dashboard':
                currentView = 'dashboard';
                await init();
                break;
            case 'switch-settings-tab':
                settingsTab = target.dataset.tab;
                await renderSettings();
                break;

            // Dashboard actions
            case 'select-task':
                await handleSelectTask(target.dataset.guid);
                break;
            case 'start-task':
                await handleStartTask(target.dataset.guid);
                break;
            case 'start-session':
                await handleStartSession();
                break;
            case 'pause-session':
                await handlePauseSession();
                break;
            case 'resume-session':
                await handleResumeSession();
                break;
            case 'complete-session':
                await handleCompleteSession();
                break;
            case 'toggle-habit':
                await handleToggleHabit(target.dataset.habit);
                break;
            case 'navigate-ref':
                await handleNavigateRef(target.dataset.guid);
                break;

            // Plugin Manager (from settings)
            case 'select-plugin':
                await handleSelectPlugin(target.dataset.plugin || target.dataset.name, target);
                break;
            case 'deselect':
                if (settingsTab === 'plugins') {
                    await handleDeselect();
                } else {
                    await handleDeselectSource();
                }
                break;
            case 'install':
                await handleInstall(target.dataset.plugin, target);
                break;
            case 'update':
                await handleUpdate(target.dataset.plugin, target);
                break;
            case 'update-all':
                await handleUpdateAll(target);
                break;
            case 'hide':
                await handleHide();
                break;
            case 'set-mode':
                await handleSetMode(target.dataset.mode);
                break;

            // Sync Manager (from settings)
            case 'select-source':
                await handleSelectSource(target.dataset.id, target);
                break;
            case 'connect':
                await handleConnect(target.dataset.source, target);
                break;
            case 'disconnect':
                await handleDisconnect(target.dataset.source, target);
                break;
            case 'sync-now':
                await handleSyncNow(target.dataset.source, target);
                break;
            case 'sync-all':
                await handleSyncAll(target);
                break;
            case 'resync-all':
                await handleResyncAll(target);
                break;
            case 'resync-source':
                await handleResyncSource(target.dataset.source, target);
                break;
            case 'rerender-all':
                await handleRerenderAll(target);
                break;
            case 'rerender-source':
                await handleRerenderSource(target.dataset.source, target);
                break;
            case 'configure':
                await handleConfigure(target.dataset.source);
                break;
            case 'close-config':
                closeConfigPanel();
                break;
            case 'save-config':
                await handleSaveConfig(target.dataset.source);
                break;
        }
    });

    app.addEventListener('change', async (e) => {
        const autoToggle = e.target.closest('[data-action="toggle-auto"]');
        if (autoToggle) {
            console.log(`Auto-update ${autoToggle.dataset.plugin}: ${autoToggle.checked}`);
            return;
        }

        const enabledToggle = e.target.closest('[data-action="toggle-enabled"]');
        if (enabledToggle) {
            const sourceId = enabledToggle.dataset.source;
            const checked = enabledToggle.checked;
            try {
                await SetSourceEnabled(sourceId, checked);
                showToast(`${sourceId} ${checked ? 'enabled' : 'disabled'}`, 'success');
            } catch (err) {
                showToast(`Failed: ${err}`, 'error');
                enabledToggle.checked = !checked;
            }
        }
    });
}

// ============================================================================
// Dashboard Handlers
// ============================================================================

async function handleSelectTask(guid) {
    // Legacy handler - now handled by start-task action
    await handleStartTask(guid);
}

async function handleStartTask(guid) {
    // If clicking the same task that's already active, do nothing
    if (sessionState.active && sessionState.taskGuid === guid) {
        return;
    }

    // Always try to pause any existing session first (backend knows the truth)
    try {
        await PauseSession();
    } catch (err) {
        // Ignore - might not have an active session
    }

    // Start a new session for this task
    try {
        await StartSession(guid);
        sessionState.active = true;
        sessionState.paused = false;
        sessionState.taskGuid = guid;
        sessionState.startTime = Date.now();
        sessionState.elapsed = 0;
        startTimerTick();
        await renderDashboard();
    } catch (err) {
        showToast(`Failed to start session: ${err}`, 'error');
        // Re-sync state from backend
        await renderDashboard();
    }
}

async function handleStartSession() {
    try {
        await StartSession(sessionState.taskGuid);
        sessionState.active = true;
        sessionState.startTime = Date.now();
        startTimerTick();
        updateFocusPanel();
    } catch (err) {
        showToast(`Failed to start: ${err}`, 'error');
    }
}

async function handlePauseSession() {
    try {
        await PauseSession();
        sessionState.active = false;
        stopTimerTick();
        await renderDashboard();
    } catch (err) {
        showToast(`Failed to pause: ${err}`, 'error');
    }
}

async function handleResumeSession() {
    try {
        await ResumeSession();
        sessionState.active = true;
        sessionState.startTime = Date.now();
        startTimerTick();
        await renderDashboard();
    } catch (err) {
        showToast(`Failed to resume: ${err}`, 'error');
    }
}

async function handleCompleteSession() {
    try {
        await CompleteSession(sessionState.taskGuid, sessionState.elapsed);
        sessionState = { active: false, taskGuid: null, taskTitle: '', elapsed: 0 };
        stopTimerTick();
        await renderDashboard();
        showToast('Task completed!', 'success');
    } catch (err) {
        showToast(`Failed to complete: ${err}`, 'error');
    }
}

async function handleToggleHabit(habitId) {
    try {
        await ToggleHabit(habitId);
        const habitEl = document.querySelector(`[data-habit="${habitId}"]`);
        if (habitEl) {
            const isDone = habitEl.classList.toggle('done');
            const check = habitEl.querySelector('.db-habit-check');
            const progress = habitEl.querySelector('.db-habit-progress');
            const isVice = habitEl.classList.contains('vice');
            if (check) check.textContent = isDone ? (isVice ? '✗' : '✓') : '';
            if (progress) progress.style.width = isDone ? '100%' : '0%';
        }
    } catch (err) {
        showToast(`Failed: ${err}`, 'error');
    }
}

async function handleNavigateRef(guid) {
    if (!guid) return;
    try {
        await NavigateToRecord(guid);
    } catch (err) {
        console.error('Failed to navigate:', err);
        showToast('Could not open in Thymer', 'error');
    }
}

// ============================================================================
// Plugin Manager Handlers (unchanged from original)
// ============================================================================

async function handleSelectPlugin(pluginId, cardElement) {
    const previousSelected = app.querySelector('.pm-card.selected');
    if (previousSelected) previousSelected.classList.remove('selected');

    const card = cardElement.closest('.pm-card');
    if (card) card.classList.add('selected');

    selectedPluginId = pluginId;

    try {
        const html = await GetPluginInfoHTML(pluginId);
        const infoPanel = document.getElementById('info-panel');
        if (infoPanel && html) infoPanel.innerHTML = html;
    } catch (err) {
        console.error('Failed to load plugin info:', err);
    }
}

async function handleDeselect() {
    const previousSelected = app.querySelector('.pm-card.selected');
    if (previousSelected) previousSelected.classList.remove('selected');
    selectedPluginId = null;

    try {
        const html = await GetDefaultInfoHTML();
        const infoPanel = document.getElementById('info-panel');
        if (infoPanel) infoPanel.innerHTML = html;
    } catch (err) {
        console.error('Failed to load default info:', err);
    }
}

async function handleInstall(pluginId, btn) {
    const card = btn.closest('.pm-card') || app.querySelector(`.pm-card[data-id="${pluginId}"]`);
    if (card) card.classList.add('loading');
    btn.disabled = true;

    try {
        await InstallPlugin(pluginId);
        showToast(`Installed ${pluginId}`, 'success');
        await init();
    } catch (err) {
        showToast(`Failed to install: ${err}`, 'error');
        if (card) card.classList.remove('loading');
        btn.disabled = false;
    }
}

async function handleUpdate(pluginId, btn) {
    const card = btn.closest('.pm-card') || app.querySelector(`.pm-card[data-id="${pluginId}"]`);
    if (card) card.classList.add('loading');
    btn.disabled = true;

    try {
        await UpdatePlugin(pluginId);
        showToast(`Updated ${pluginId}`, 'success');
        await init();
    } catch (err) {
        showToast(`Failed to update: ${err}`, 'error');
        if (card) card.classList.remove('loading');
        btn.disabled = false;
    }
}

async function handleUpdateAll(btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Update All';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Updating...';

    try {
        await UpdateAllPlugins();
        showToast('All plugins updated', 'success');
        await init();
    } catch (err) {
        showToast(`Failed to update: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleHide() {
    try {
        await HideWindow();
    } catch (err) {
        console.error('Failed to hide window:', err);
    }
}

async function handleSetMode(mode) {
    const isDev = mode === 'dev';
    await SetDevMode(isDev);
    await init();
}

// ============================================================================
// Sync Manager Handlers (unchanged from original)
// ============================================================================

async function handleSelectSource(sourceId, cardElement) {
    const previousSelected = app.querySelector('.sm-card.selected');
    if (previousSelected) previousSelected.classList.remove('selected');

    const card = cardElement.closest('.sm-card');
    if (card) card.classList.add('selected');

    selectedSourceId = sourceId;

    try {
        const html = await GetSyncSourceInfoHTML(sourceId);
        const infoPanel = document.getElementById('sync-info-panel');
        if (infoPanel && html) infoPanel.innerHTML = html;
    } catch (err) {
        console.error('Failed to load source info:', err);
    }
}

async function handleDeselectSource() {
    const previousSelected = app.querySelector('.sm-card.selected');
    if (previousSelected) previousSelected.classList.remove('selected');
    selectedSourceId = null;

    try {
        const html = await GetSyncDefaultInfoHTML();
        const infoPanel = document.getElementById('sync-info-panel');
        if (infoPanel) infoPanel.innerHTML = html;
    } catch (err) {
        console.error('Failed to load default info:', err);
    }
}

async function handleConnect(sourceId, btn) {
    if (sourceId === 'github') {
        showTokenDialog('github', 'GitHub Personal Access Token',
            'Create a token at GitHub → Settings → Developer settings → Personal access tokens (classic) with "repo" scope.',
            'ghp_xxxxxxxxxxxx'
        );
        return;
    }

    if (sourceId === 'google-calendar' || sourceId === 'google-people') {
        const hasCredentials = await HasGoogleCredentials();
        if (hasCredentials) {
            btn.disabled = true;
            btn.textContent = 'Connecting...';
            try {
                await ConnectSource(sourceId);
                showToast(`Connected to ${sourceId}`, 'success');
                await init();
            } catch (err) {
                showToast(`${err}`, 'error');
            }
            btn.disabled = false;
            btn.textContent = 'Connect';
        } else {
            showGoogleCredentialsDialog();
        }
        return;
    }

    btn.disabled = true;
    const originalText = btn.textContent;
    btn.textContent = 'Connecting...';

    try {
        await ConnectSource(sourceId);
        showToast(`Connected to ${sourceId}`, 'success');
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) infoPanel.innerHTML = html;
        }
    } catch (err) {
        showToast(`${err}`, 'error');
    }

    btn.disabled = false;
    btn.textContent = originalText;
}

async function handleDisconnect(sourceId, btn) {
    btn.disabled = true;
    try {
        await DisconnectSource(sourceId);
        showToast(`Disconnected from ${sourceId}`, 'success');
        await init();
    } catch (err) {
        showToast(`Failed: ${err}`, 'error');
        btn.disabled = false;
    }
}

async function handleSyncNow(sourceId, btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Sync Now';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Syncing...';

    try {
        await SyncNow(sourceId);
        showToast(`Synced ${sourceId}`, 'success');
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) infoPanel.innerHTML = html;
        }
    } catch (err) {
        showToast(`Sync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleSyncAll(btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Sync All';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Syncing...';

    try {
        await SyncAll();
        showToast('All sources synced', 'success');
        await init();
    } catch (err) {
        showToast(`Sync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleResyncAll(btn) {
    if (!confirm('This will clear all synced data and fetch everything fresh. Continue?')) return;

    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Full Resync';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Clearing & Syncing...';

    try {
        await ResyncAll();
        showToast('Full resync complete', 'success');
        await init();
    } catch (err) {
        showToast(`Resync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleRerenderAll(btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Rerender All';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Rendering...';

    try {
        await RerenderAll();
        showToast('All items re-rendered to Thymer', 'success');
        await init();
    } catch (err) {
        showToast(`Rerender failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleResyncSource(sourceId, btn) {
    if (!confirm(`This will clear all data for ${sourceId} and fetch everything fresh. Continue?`)) return;

    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Full Resync';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Clearing & Syncing...';

    try {
        await ResyncSource(sourceId);
        showToast(`Full resync complete for ${sourceId}`, 'success');
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) infoPanel.innerHTML = html;
        }
    } catch (err) {
        showToast(`Resync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleRerenderSource(sourceId, btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Rerender';
    if (btn.querySelector('span')) btn.querySelector('span').textContent = 'Rendering...';

    try {
        await RerenderSource(sourceId);
        showToast(`Re-rendered ${sourceId} to Thymer`, 'success');
    } catch (err) {
        showToast(`Rerender failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) btn.querySelector('span').textContent = originalText;
}

async function handleConfigure(sourceId) {
    try {
        let html;
        if (sourceId === 'github') {
            html = await GetGitHubConfigHTML();
        } else if (sourceId === 'google-calendar') {
            html = await GetGoogleCalendarConfigHTML();
        } else {
            showToast(`No configuration for ${sourceId}`, 'info');
            return;
        }

        const overlay = document.createElement('div');
        overlay.className = 'sm-overlay';

        const container = document.createElement('div');
        container.innerHTML = html;
        const panel = container.firstElementChild;

        document.body.appendChild(overlay);
        document.body.appendChild(panel);

        overlay.addEventListener('click', closeConfigPanel);

        panel.addEventListener('click', async (e) => {
            const target = e.target.closest('[data-action]');
            if (!target) return;

            const action = target.dataset.action;
            if (action === 'close-config') {
                closeConfigPanel();
            } else if (action === 'save-config') {
                await handleSaveConfig(target.dataset.source);
            } else if (action === 'refresh-repos') {
                await handleRefreshRepos();
            }
        });

        const filterInput = panel.querySelector('.sm-filter-input');
        if (filterInput) {
            filterInput.addEventListener('input', (e) => {
                const query = e.target.value.toLowerCase();
                const items = panel.querySelectorAll('.sm-repo-item, .sm-calendar-item');
                items.forEach(item => {
                    const name = item.dataset.repoName || item.dataset.calendarName || '';
                    item.classList.toggle('hidden', !name.toLowerCase().includes(query));
                });
            });
            filterInput.focus();
        }
    } catch (err) {
        showToast(`Failed to load config: ${err}`, 'error');
    }
}

async function handleRefreshRepos() {
    showToast('Fetching repositories...', 'info');
    try {
        const html = await GetGitHubConfigHTML();
        const panel = document.querySelector('.sm-config-panel');
        if (panel) {
            const container = document.createElement('div');
            container.innerHTML = html;
            const newPanel = container.firstElementChild;

            newPanel.addEventListener('click', async (e) => {
                const target = e.target.closest('[data-action]');
                if (!target) return;
                const action = target.dataset.action;
                if (action === 'close-config') closeConfigPanel();
                else if (action === 'save-config') await handleSaveConfig(target.dataset.source);
                else if (action === 'refresh-repos') await handleRefreshRepos();
            });

            panel.replaceWith(newPanel);
        }
    } catch (err) {
        showToast(`Failed to refresh: ${err}`, 'error');
    }
}

function closeConfigPanel() {
    const overlay = document.querySelector('.sm-overlay');
    const panel = document.querySelector('.sm-config-panel');
    if (overlay) overlay.remove();
    if (panel) panel.remove();
}

async function handleSaveConfig(sourceId) {
    try {
        if (sourceId === 'github') {
            const repos = [];
            document.querySelectorAll('.sm-repo-item input:checked').forEach(input => {
                repos.push(input.dataset.repo);
            });
            await SetGitHubRepos(repos);
        } else if (sourceId === 'google-calendar') {
            const calendars = [];
            document.querySelectorAll('.sm-calendar-item input:checked').forEach(input => {
                calendars.push(input.dataset.calendar);
            });
            await SetGoogleCalendars(calendars);
        }

        showToast('Configuration saved', 'success');
        closeConfigPanel();

        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) infoPanel.innerHTML = html;
        }
    } catch (err) {
        showToast(`Failed to save: ${err}`, 'error');
    }
}

// Token dialogs (same as original)
function showTokenDialog(sourceId, title, hint, placeholder) {
    const overlay = document.createElement('div');
    overlay.className = 'sm-overlay';

    const dialog = document.createElement('div');
    dialog.className = 'sm-config-panel sm-token-dialog';
    dialog.innerHTML = `
        <div class="sm-config-header">
            <h3>${title}</h3>
            <button class="sm-config-close" data-action="close-token">&times;</button>
        </div>
        <div class="sm-config-body">
            <p class="sm-token-hint">${hint}</p>
            <input type="password" class="sm-token-input" placeholder="${placeholder}" autocomplete="off" spellcheck="false">
            <div class="sm-token-actions">
                <button class="sm-btn-small" data-action="close-token">Cancel</button>
                <button class="sm-btn-small primary" data-action="save-token" data-source="${sourceId}">Connect</button>
            </div>
        </div>
    `;

    document.body.appendChild(overlay);
    document.body.appendChild(dialog);

    const input = dialog.querySelector('.sm-token-input');
    input.focus();

    input.addEventListener('keydown', async (e) => {
        if (e.key === 'Enter') await saveToken(sourceId, input.value);
        else if (e.key === 'Escape') closeTokenDialog();
    });

    overlay.addEventListener('click', closeTokenDialog);
    dialog.addEventListener('click', async (e) => {
        const target = e.target.closest('[data-action]');
        if (!target) return;
        if (target.dataset.action === 'close-token') closeTokenDialog();
        else if (target.dataset.action === 'save-token') await saveToken(target.dataset.source, input.value);
    });
}

function closeTokenDialog() {
    const overlay = document.querySelector('.sm-overlay');
    const dialog = document.querySelector('.sm-token-dialog');
    if (overlay) overlay.remove();
    if (dialog) dialog.remove();
}

function showGoogleCredentialsDialog() {
    const overlay = document.createElement('div');
    overlay.className = 'sm-overlay';

    const dialog = document.createElement('div');
    dialog.className = 'sm-config-panel sm-token-dialog sm-google-dialog';
    dialog.innerHTML = `
        <div class="sm-config-header">
            <h3>Google Cloud Credentials</h3>
            <button class="sm-config-close" data-action="close-google">&times;</button>
        </div>
        <div class="sm-config-body">
            <p class="sm-token-hint">
                Paste the contents of your OAuth credentials JSON file from Google Cloud Console.
                <br><br>
                <strong>Setup:</strong> Google Cloud Console → APIs & Services → Credentials → Create OAuth client ID → Desktop app → Download JSON
            </p>
            <textarea class="sm-json-input" placeholder='{"installed":{"client_id":"...","client_secret":"..."}}'></textarea>
            <div class="sm-token-actions">
                <button class="sm-btn-small" data-action="close-google">Cancel</button>
                <button class="sm-btn-small primary" data-action="save-google">Connect</button>
            </div>
        </div>
    `;

    document.body.appendChild(overlay);
    document.body.appendChild(dialog);

    const textarea = dialog.querySelector('.sm-json-input');
    textarea.focus();

    textarea.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') closeGoogleDialog();
    });

    overlay.addEventListener('click', closeGoogleDialog);
    dialog.addEventListener('click', async (e) => {
        const target = e.target.closest('[data-action]');
        if (!target) return;
        if (target.dataset.action === 'close-google') closeGoogleDialog();
        else if (target.dataset.action === 'save-google') await saveGoogleCredentials(textarea.value);
    });
}

function closeGoogleDialog() {
    const overlay = document.querySelector('.sm-overlay');
    const dialog = document.querySelector('.sm-google-dialog');
    if (overlay) overlay.remove();
    if (dialog) dialog.remove();
}

async function saveGoogleCredentials(jsonStr) {
    if (!jsonStr.trim()) {
        showToast('Please paste your credentials JSON', 'error');
        return;
    }

    const saveBtn = document.querySelector('[data-action="save-google"]');
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.textContent = 'Connecting...';
    }

    try {
        await SetGoogleCredentials(jsonStr.trim());
        showToast('Connected to Google', 'success');
        closeGoogleDialog();
        await init();
    } catch (err) {
        showToast(`${err}`, 'error');
        if (saveBtn) {
            saveBtn.disabled = false;
            saveBtn.textContent = 'Connect';
        }
    }
}

async function saveToken(sourceId, token) {
    if (!token.trim()) {
        showToast('Please enter a token', 'error');
        return;
    }

    const saveBtn = document.querySelector('[data-action="save-token"]');
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.textContent = 'Validating...';
    }

    try {
        if (sourceId === 'github') {
            await SetGitHubToken(token.trim());
        }
        showToast(`Connected to ${sourceId}`, 'success');
        closeTokenDialog();
        await init();
    } catch (err) {
        showToast(`${err}`, 'error');
        if (saveBtn) {
            saveBtn.disabled = false;
            saveBtn.textContent = 'Connect';
        }
    }
}

// Start the app
init();
