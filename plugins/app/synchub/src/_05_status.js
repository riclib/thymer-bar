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
      htmlLabel: this.buildLabel('connecting'),
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
      error: 'SyncHub - Connection failed (click to retry)',
    };

    statusBarItem.setTooltip(tooltips[state] || 'SyncHub');
  },

  /**
   * Build HTML label for status bar.
   * Uses ti-cloud for all states, CSS classes handle color:
   * - connected: green
   * - connecting: default/gray
   * - disconnected/error: amber
   */
  buildLabel(state) {
    return `<span class="ti ti-cloud synchub-status ${state}"></span>`;
  },
};
