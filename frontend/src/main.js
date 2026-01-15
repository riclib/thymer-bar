// thymer-bar Plugin Manager & Sync Manager

import {
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
    GetGitHubConfigHTML,
    SetGitHubRepos,
    GetGoogleCalendarConfigHTML,
    SetGoogleCalendars
} from '../wailsjs/go/main/App';

import { EventsOn } from '../wailsjs/runtime/runtime';

const app = document.getElementById('app');
let currentView = 'plugins'; // 'plugins' | 'sync'
let selectedPluginId = null;
let selectedSourceId = null;

// Listen for connection status changes
EventsOn('thymer:connection', (data) => {
    updateConnectionStatus(data.connected, data.count);

    // Refresh UI when first connection established (to get installed versions)
    if (data.connected && data.count === 1) {
        init();
    }
});

// Listen for sync progress updates
EventsOn('sync:progress', (data) => {
    updateSyncProgress(data);
});

// Track current sync progress UI elements
let syncProgressElements = {};

function updateSyncProgress(progress) {
    const { source, phase, processed, created, updated, unchanged, errors } = progress;

    // Find or create progress indicator for this source
    const card = document.querySelector(`[data-source="${source}"]`);
    if (!card) return;

    const btn = card.querySelector('[data-action="sync-now"]');
    if (!btn) return;

    // Update button text with progress
    const span = btn.querySelector('span');
    if (span) {
        if (phase === 'complete') {
            span.textContent = 'Sync Now';
        } else if (phase === 'error') {
            span.textContent = 'Error';
        } else {
            const total = created + updated + unchanged;
            span.textContent = `${phase}... ${processed} (${created}↑ ${updated}↻ ${unchanged}=)`;
        }
    }

    // Update info panel if this source is selected
    if (selectedSourceId === source) {
        const progressEl = document.getElementById('sync-progress');
        if (progressEl) {
            if (phase === 'complete') {
                progressEl.innerHTML = `<span class="sync-done">✓ Done: ${created} new, ${updated} updated, ${unchanged} unchanged</span>`;
                setTimeout(() => {
                    if (progressEl) progressEl.innerHTML = '';
                }, 3000);
            } else if (phase === 'error') {
                progressEl.innerHTML = `<span class="sync-error">✗ Error: ${errors} errors</span>`;
            } else {
                progressEl.innerHTML = `
                    <span class="sync-phase">${phase}</span>
                    <span class="sync-stats">${processed} processed | ${created}↑ ${updated}↻ ${unchanged}= | ${errors} errors</span>
                `;
            }
        }
    }
}

function updateConnectionStatus(connected, count) {
    // Update status dot in info panel
    const statusDot = document.querySelector('.pm-status-dot');
    const statusText = statusDot?.nextElementSibling;

    if (statusDot) {
        statusDot.classList.remove('connected', 'disconnected');
        statusDot.classList.add(connected ? 'connected' : 'disconnected');
    }
    if (statusText) {
        // Only show count if > 1 (indicates a problem)
        statusText.textContent = connected
            ? (count > 1 ? `Connected (${count})` : 'Connected')
            : 'Disconnected';
    }
}

// Initialize the app
async function init() {
    try {
        const contentHtml = currentView === 'sync'
            ? await GetSyncManagerHTML()
            : await GetPluginManagerHTML();

        // Wrap with tab navigation
        app.innerHTML = `
            <div class="tb-tabs">
                <button class="tb-tab ${currentView === 'plugins' ? 'active' : ''}" data-action="switch-view" data-view="plugins">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
                    Plugins
                </button>
                <button class="tb-tab ${currentView === 'sync' ? 'active' : ''}" data-action="switch-view" data-view="sync">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 21h5v-5"/></svg>
                    Sync
                </button>
            </div>
            <div class="tb-content">
                ${contentHtml}
            </div>
        `;

        selectedPluginId = null;
        selectedSourceId = null;
        bindEvents();
    } catch (err) {
        console.error('Failed to load:', err);
        app.innerHTML = `
            <div class="pm-empty">
                <p>Failed to load</p>
                <p>${err}</p>
            </div>
        `;
    }
}

// Track if events are bound to prevent duplicate listeners
let eventsBound = false;

// Bind event listeners using event delegation (only once)
function bindEvents() {
    if (eventsBound) return;
    eventsBound = true;

    app.addEventListener('click', async (e) => {
        const target = e.target.closest('[data-action]');
        if (!target) return;

        const action = target.dataset.action;
        const pluginId = target.dataset.plugin;
        const pluginName = target.dataset.name;
        const sourceId = target.dataset.source;
        const view = target.dataset.view;

        switch (action) {
            // View switching
            case 'switch-view':
                if (view && view !== currentView) {
                    currentView = view;
                    await init();
                }
                break;

            // Plugin Manager actions
            case 'select-plugin':
                await handleSelectPlugin(pluginId || pluginName, target);
                break;
            case 'deselect':
                if (currentView === 'plugins') {
                    await handleDeselect();
                } else {
                    await handleDeselectSource();
                }
                break;
            case 'install':
                await handleInstall(pluginId, target);
                break;
            case 'update':
                await handleUpdate(pluginId, target);
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

            // Sync Manager actions
            case 'select-source':
                await handleSelectSource(target.dataset.id, target);
                break;
            case 'connect':
                await handleConnect(sourceId, target);
                break;
            case 'disconnect':
                await handleDisconnect(sourceId, target);
                break;
            case 'sync-now':
                await handleSyncNow(sourceId, target);
                break;
            case 'sync-all':
                await handleSyncAll(target);
                break;
            case 'configure':
                await handleConfigure(sourceId);
                break;
            case 'close-config':
                closeConfigPanel();
                break;
            case 'save-config':
                await handleSaveConfig(sourceId);
                break;
        }
    });

    app.addEventListener('change', async (e) => {
        // Plugin auto-update toggle
        const autoToggle = e.target.closest('[data-action="toggle-auto"]');
        if (autoToggle) {
            const pluginId = autoToggle.dataset.plugin;
            const checked = autoToggle.checked;
            console.log(`Auto-update ${pluginId}: ${checked}`);
            // TODO: Save preference
            return;
        }

        // Source enabled toggle
        const enabledToggle = e.target.closest('[data-action="toggle-enabled"]');
        if (enabledToggle) {
            const sourceId = enabledToggle.dataset.source;
            const checked = enabledToggle.checked;
            try {
                await SetSourceEnabled(sourceId, checked);
                showToast(`${sourceId} ${checked ? 'enabled' : 'disabled'}`, 'success');
            } catch (err) {
                showToast(`Failed: ${err}`, 'error');
                enabledToggle.checked = !checked; // Revert
            }
            return;
        }
    });
}

async function handleSelectPlugin(pluginId, cardElement) {
    // Update selection state
    const previousSelected = app.querySelector('.pm-card.selected');
    if (previousSelected) {
        previousSelected.classList.remove('selected');
    }

    const card = cardElement.closest('.pm-card');
    if (card) {
        card.classList.add('selected');
    }

    selectedPluginId = pluginId;

    // Update info panel
    try {
        const html = await GetPluginInfoHTML(pluginId);
        const infoPanel = document.getElementById('info-panel');
        if (infoPanel && html) {
            infoPanel.innerHTML = html;
        }
    } catch (err) {
        console.error('Failed to load plugin info:', err);
    }
}

async function handleDeselect() {
    const previousSelected = app.querySelector('.pm-card.selected');
    if (previousSelected) {
        previousSelected.classList.remove('selected');
    }

    selectedPluginId = null;

    try {
        const html = await GetDefaultInfoHTML();
        const infoPanel = document.getElementById('info-panel');
        if (infoPanel) {
            infoPanel.innerHTML = html;
        }
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
        // Refresh UI
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
        // Refresh to show new version
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
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = 'Updating...';
    }

    try {
        await UpdateAllPlugins();
        showToast('All plugins updated', 'success');
        // Refresh to show new versions
        await init();
    } catch (err) {
        showToast(`Failed to update: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = originalText;
    }
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
    // Refresh UI to reflect mode change
    await init();
}

function showToast(message, type = 'info') {
    // Remove existing toast
    const existing = document.querySelector('.pm-toast');
    if (existing) existing.remove();

    const toast = document.createElement('div');
    toast.className = `pm-toast ${type}`;
    toast.innerHTML = `
        ${type === 'success' ? '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><path d="m9 11 3 3L22 4"/></svg>' : ''}
        ${type === 'error' ? '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>' : ''}
        <span>${message}</span>
    `;
    document.body.appendChild(toast);

    // Auto-remove after 3 seconds
    setTimeout(() => toast.remove(), 3000);
}

// ============================================================================
// Sync Manager Handlers
// ============================================================================

async function handleSelectSource(sourceId, cardElement) {
    // Update selection state
    const previousSelected = app.querySelector('.sm-card.selected');
    if (previousSelected) {
        previousSelected.classList.remove('selected');
    }

    const card = cardElement.closest('.sm-card');
    if (card) {
        card.classList.add('selected');
    }

    selectedSourceId = sourceId;

    // Update info panel
    try {
        const html = await GetSyncSourceInfoHTML(sourceId);
        const infoPanel = document.getElementById('sync-info-panel');
        if (infoPanel && html) {
            infoPanel.innerHTML = html;
        }
    } catch (err) {
        console.error('Failed to load source info:', err);
    }
}

async function handleDeselectSource() {
    const previousSelected = app.querySelector('.sm-card.selected');
    if (previousSelected) {
        previousSelected.classList.remove('selected');
    }

    selectedSourceId = null;

    try {
        const html = await GetSyncDefaultInfoHTML();
        const infoPanel = document.getElementById('sync-info-panel');
        if (infoPanel) {
            infoPanel.innerHTML = html;
        }
    } catch (err) {
        console.error('Failed to load default info:', err);
    }
}

async function handleConnect(sourceId, btn) {
    btn.disabled = true;
    const originalText = btn.textContent;
    btn.textContent = 'Connecting...';

    try {
        await ConnectSource(sourceId);
        showToast(`Connected to ${sourceId}`, 'success');
        // Refresh info panel
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) {
                infoPanel.innerHTML = html;
            }
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
        // Refresh
        await init();
    } catch (err) {
        showToast(`Failed: ${err}`, 'error');
        btn.disabled = false;
    }
}

async function handleSyncNow(sourceId, btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Sync Now';
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = 'Syncing...';
    }

    try {
        await SyncNow(sourceId);
        showToast(`Synced ${sourceId}`, 'success');
        // Refresh info panel
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) {
                infoPanel.innerHTML = html;
            }
        }
    } catch (err) {
        showToast(`Sync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = originalText;
    }
}

async function handleSyncAll(btn) {
    btn.disabled = true;
    const originalText = btn.querySelector('span')?.textContent || 'Sync All';
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = 'Syncing...';
    }

    try {
        await SyncAll();
        showToast('All sources synced', 'success');
        await init(); // Refresh view
    } catch (err) {
        showToast(`Sync failed: ${err}`, 'error');
    }

    btn.disabled = false;
    if (btn.querySelector('span')) {
        btn.querySelector('span').textContent = originalText;
    }
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

        // Show config panel
        const overlay = document.createElement('div');
        overlay.className = 'sm-overlay';

        const container = document.createElement('div');
        container.innerHTML = html;
        const panel = container.firstElementChild;

        document.body.appendChild(overlay);
        document.body.appendChild(panel);

        // Bind events for modal (since it's outside #app)
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

        // Filter input handler
        const filterInput = panel.querySelector('.sm-filter-input');
        if (filterInput) {
            filterInput.addEventListener('input', (e) => {
                const query = e.target.value.toLowerCase();
                const items = panel.querySelectorAll('.sm-repo-item, .sm-calendar-item');
                items.forEach(item => {
                    const name = item.dataset.repoName || item.dataset.calendarName || '';
                    if (name.toLowerCase().includes(query)) {
                        item.classList.remove('hidden');
                    } else {
                        item.classList.add('hidden');
                    }
                });
            });
            // Focus the filter input
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

            // Re-bind events
            newPanel.addEventListener('click', async (e) => {
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

        // Refresh source info
        if (selectedSourceId === sourceId) {
            const html = await GetSyncSourceInfoHTML(sourceId);
            const infoPanel = document.getElementById('sync-info-panel');
            if (infoPanel && html) {
                infoPanel.innerHTML = html;
            }
        }
    } catch (err) {
        showToast(`Failed to save: ${err}`, 'error');
    }
}

// Start the app
init();
