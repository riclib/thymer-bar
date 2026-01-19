/**
 * SyncHub - Desktop Bridge + Tool Registry
 *
 * Lean plugin that:
 * 1. Connects to thymer-bar via WebSocket
 * 2. Provides tool registry for collection plugins
 * 3. Exposes shared markdown utilities
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    // Find our collection
    const collections = await this.data.getAllCollections();
    this.myCollection = collections.find((c) => c.getName() === this.getName());

    if (!this.myCollection) {
      console.error('[SyncHub] Could not find own collection!');
      return;
    }

    // Connection state
    this.ws = null;
    this.wsPort = this.getConfiguration().custom?.ws_port || 8496;
    this.reconnectDelays = [5000, 30000, 120000]; // 5s, 30s, 2m then stop
    this.reconnectAttempt = 0;
    this.collectionTools = new Map();

    // Expose API for other plugins
    window.syncHub = {
      // Tool registry
      registerCollectionTools: (config) => this.registerCollectionTools(config),
      getRegisteredTools: () => this.getRegisteredTools(),
      executeToolCall: (name, args) => this.executeToolCall(name, args),

      // Markdown utilities
      insertMarkdown: (markdown, record, afterItem) =>
        SyncHubMarkdown.insert(markdown, record, afterItem),
      replaceContents: (markdown, record) =>
        SyncHubMarkdown.replace(markdown, record),
      parseLine: (line) => SyncHubMarkdown.parseLine(line),
      parseInlineFormatting: (text) => SyncHubMarkdown.parseInline(text),

      // Journal integration
      getTodayJournal: () => SyncHubHelpers.getTodayJournal(this.data),

      // Desktop bridge
      isConnected: () => this.ws?.readyState === WebSocket.OPEN,
      send: (msg) => SyncHubConnect.send(this.ws, msg),
    };

    // Dispatch ready event for collection plugins
    window.dispatchEvent(new CustomEvent('synchub-ready'));

    // Initialize PlannerHub sidebar
    SyncHubPlanner.init(this.ui, this.data);

    // Connect to thymer-bar
    this.connect();

    // Status bar indicator
    this.statusBarItem = SyncHubStatus.create(this.ui, () => this.onStatusBarClick());

    // Command palette: Paste Markdown
    this.pasteCommand = this.ui.addCommandPaletteCommand({
      label: 'Paste Markdown',
      icon: 'clipboard-text',
      onSelected: () => this.pasteMarkdownFromClipboard(),
    });

    // Initialize daily note watcher (SPIKE)
    DailyNoteWatcher.init(this.ui, this.data);
  }

  onUnload() {
    if (this.ws && typeof this.ws.close === 'function') {
      this.ws.close();
    }
    this.ws = null;
    if (this.statusBarItem) {
      this.statusBarItem.remove();
    }
    if (this.pasteCommand) {
      this.pasteCommand.remove();
    }
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
    }
    // Cleanup planner
    if (SyncHubPlanner.sidebarItem) {
      SyncHubPlanner.sidebarItem.remove();
    }
    SyncHubPlanner.hide();
    // Cleanup daily note watcher (SPIKE)
    DailyNoteWatcher.cleanup();
    delete window.syncHub;
    delete window.plannerHub;
  }

  // ===========================================================================
  // Connection
  // ===========================================================================

  connect() {
    SyncHubConnect.connect({
      ws: this.ws,
      wsPort: this.wsPort,
      updateStatus: (state) => this.updateStatus(state),
      onConnected: () => {
        this.reconnectAttempt = 0; // Reset on successful connection
        SyncHubConnect.send(this.ws, {
          type: 'register',
          client: 'synchub',
          capabilities: ['tools', 'navigate'],
        });
        this.sendToolsManifest();
        // Send daily note snapshot on connect
        if (typeof DailyNoteWatcher !== 'undefined' && DailyNoteWatcher.sendSnapshot) {
          setTimeout(() => DailyNoteWatcher.sendSnapshot(false), 500);
        }
      },
      onMessage: (data) => this.handleMessage(data),
      scheduleReconnect: () => this.scheduleReconnect(),
    });
    // Update ws reference after connect
    this.ws = window._syncHubWS;
  }

  scheduleReconnect() {
    if (this.reconnectAttempt >= this.reconnectDelays.length) {
      // Give up - user can click status bar to retry
      return;
    }
    const delay = this.reconnectDelays[this.reconnectAttempt];
    this.reconnectAttempt++;
    this.reconnectTimeout = setTimeout(() => this.connect(), delay);
  }

  handleMessage(data) {
    // Get workspace GUID from an existing panel's navigation state
    const panels = this.ui?.getPanels() || [];
    const workspaceGuid = panels[0]?.getNavigation()?.workspaceGuid || null;

    SyncHubConnect.handleMessage(data, {
      ws: this.ws,
      data: this.data,
      ui: this.ui,
      collectionTools: this.collectionTools,
      sendToolsManifest: () => this.sendToolsManifest(),
      workspaceGuid,
    });
  }

  sendToolsManifest() {
    const tools = this.getRegisteredTools();
    SyncHubConnect.send(this.ws, {
      type: 'tools',
      tools: tools.map((t) => ({
        name: t.function.name,
        description: t.function.description,
        parameters: t.function.parameters,
      })),
    });
  }

  // ===========================================================================
  // Tool Registry
  // ===========================================================================

  registerCollectionTools(config) {
    SyncHubRegistry.register(this.collectionTools, config, () => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.sendToolsManifest();
      }
    });
  }

  getRegisteredTools() {
    return SyncHubRegistry.getAllTools(this.collectionTools);
  }

  async executeToolCall(name, args) {
    return SyncHubRegistry.execute(name, args, {
      data: this.data,
      ui: this.ui,
      collectionTools: this.collectionTools,
    });
  }

  // ===========================================================================
  // Status Bar
  // ===========================================================================

  updateStatus(state) {
    SyncHubStatus.update(this.statusBarItem, state);
  }

  onStatusBarClick() {
    if (this.ws?.readyState !== WebSocket.OPEN) {
      this.reconnectAttempt = 0; // Reset on manual retry
      this.connect();
    }
  }

  // ===========================================================================
  // Commands
  // ===========================================================================

  async pasteMarkdownFromClipboard() {
    try {
      const markdown = await navigator.clipboard.readText();
      if (!markdown || !markdown.trim()) {
        this.ui.addToaster({
          title: 'Paste Markdown',
          message: 'Clipboard is empty',
          dismissible: true,
          autoDestroyTime: 2000,
        });
        return;
      }

      const record = this.ui.getActivePanel()?.getActiveRecord();
      if (!record) {
        this.ui.addToaster({
          title: 'Paste Markdown',
          message: 'No active record. Open a note first.',
          dismissible: true,
          autoDestroyTime: 3000,
        });
        return;
      }

      const lineItems = (await record.getLineItems()) || [];
      const topLevel = lineItems.filter((item) => item.parent_guid === record.guid);
      const lastItem = topLevel.length > 0 ? topLevel[topLevel.length - 1] : null;

      const count = await SyncHubMarkdown.insert(markdown, record, lastItem);
      this.ui.addToaster({
        title: 'Paste Markdown',
        message: `Inserted ${count} item${count !== 1 ? 's' : ''}`,
        dismissible: true,
        autoDestroyTime: 2000,
      });
    } catch (error) {
      this.ui.addToaster({
        title: 'Paste Markdown',
        message: `Failed: ${error.message}`,
        dismissible: true,
        autoDestroyTime: 3000,
      });
    }
  }
}
