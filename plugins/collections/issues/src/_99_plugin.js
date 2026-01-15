/**
 * Issues Collection - Collection Plugin
 *
 * Provides query tools for the Issues collection.
 * Works with any source: GitHub, GitLab, Jira, Linear, etc.
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    // Wait for SyncHub to register tools
    window.addEventListener('synchub-ready', () => this.registerTools(), { once: true });
    if (window.syncHub) this.registerTools();
  }

  registerTools() {
    if (!window.syncHub?.registerCollectionTools) return;

    const ver = this.getConfiguration()?.ver || 1;
    window.syncHub.registerCollectionTools(IssuesTools.getDefinitions(ver));
  }
}
