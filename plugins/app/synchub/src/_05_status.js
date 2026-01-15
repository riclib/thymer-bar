/**
 * SyncHub Status Bar
 * Manages the status bar indicator in Thymer.
 */
const SyncHubStatus = {
  /**
   * Create a status bar item.
   */
  create(ui, onClick) {
    return ui.addStatusBarItem({
      htmlLabel: this.buildLabel('disconnected'),
      tooltip: 'SyncHub - Connecting...',
      onClick,
    });
  },

  /**
   * Update status bar state.
   */
  update(statusBarItem, state) {
    if (!statusBarItem) return;

    statusBarItem.setHtmlLabel(this.buildLabel(state));

    const tooltips = {
      connected: 'SyncHub - Connected to thymer-bar',
      disconnected: 'SyncHub - Disconnected (click to retry)',
      connecting: 'SyncHub - Connecting...',
    };

    statusBarItem.setTooltip(tooltips[state] || 'SyncHub');
  },

  /**
   * Build HTML label for status bar.
   */
  buildLabel(state) {
    const icon = state === 'disconnected' ? 'ti-cloud-off' : 'ti-cloud';
    return `<span class="ti ${icon} synchub-status ${state}"></span>`;
  },
};
