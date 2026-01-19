/**
 * SyncHub WebSocket Connection
 * Manages connection to thymer-bar desktop app.
 */
const SyncHubConnect = {
  /**
   * Connect to thymer-bar WebSocket.
   */
  connect(ctx) {
    const { wsPort, updateStatus, onMessage, onConnected, scheduleReconnect } = ctx;

    // Use shared connection to prevent multiple instances
    if (window._syncHubWS) {
      const state = window._syncHubWS.readyState;
      if (state === WebSocket.OPEN || state === WebSocket.CONNECTING) {
        ctx.ws = window._syncHubWS;
        if (state === WebSocket.OPEN) {
          updateStatus('connected');
        }
        return;
      }
    }

    updateStatus('connecting');

    try {
      const ws = new WebSocket(`ws://127.0.0.1:${wsPort}/ws`);
      window._syncHubWS = ws;
      ctx.ws = ws;

      ws.onopen = () => {
        updateStatus('connected');
        if (onConnected) onConnected();
      };

      ws.onclose = (event) => {
        window._syncHubWS = null;
        // Use 'error' if we never connected (code 1006 = abnormal closure)
        // Use 'disconnected' if we were previously connected
        const wasConnected = event.wasClean || event.code === 1000;
        updateStatus(wasConnected ? 'disconnected' : 'error');
        scheduleReconnect();
      };

      ws.onerror = () => {
        // Error state will be set in onclose
      };

      ws.onmessage = (event) => {
        if (onMessage) onMessage(event.data);
      };
    } catch (err) {
      updateStatus('error');
      scheduleReconnect();
    }
  },

  /**
   * Send a message via WebSocket.
   */
  send(ws, msg) {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  },

  /**
   * Handle incoming messages from thymer-bar.
   */
  async handleMessage(data, ctx) {
    const { ws, data: dataAPI, ui, collectionTools, sendToolsManifest } = ctx;

    try {
      const msg = JSON.parse(data);
      console.log('[SyncHub] Received:', msg.type, msg.action || '', msg.id || '');

      switch (msg.type) {
        case 'tool_call':
          await this.handleToolCall(msg, ctx);
          break;
        case 'request':
          await this.handleRequest(msg, ctx);
          break;
        case 'navigate':
          this.handleNavigate(msg, ctx);
          break;
        case 'get_tools':
          sendToolsManifest();
          break;
        case 'ping':
          this.send(ws, { type: 'pong' });
          break;
        case 'ack':
          break;
        case 'install_plugin':
          await this.handleInstallPlugin(msg, ctx);
          break;
        case 'get_installed_plugins':
          await this.handleGetInstalledPlugins(msg, ctx);
          break;
        default:
          console.warn('[SyncHub] Unknown message type:', msg.type);
      }
    } catch (err) {
      console.error('[SyncHub] Message handling error:', err);
    }
  },

  async handleToolCall(msg, ctx) {
    const { ws, data, ui, collectionTools } = ctx;
    const { id, name, args } = msg;
    try {
      const result = await SyncHubRegistry.execute(name, args || {}, { data, ui, collectionTools });
      this.send(ws, { type: 'tool_result', id, result });
    } catch (err) {
      this.send(ws, { type: 'tool_result', id, error: err.message });
    }
  },

  async handleRequest(msg, ctx) {
    const { ws, data } = ctx;
    const { id, action } = msg;
    // msg.data is already an object (from Go's json.RawMessage)
    const payload = msg.data || {};

    console.log('[SyncHub] Request:', action, 'id:', id);

    try {
      let result;
      switch (action) {
        case 'syncRecord':
          result = await this.handleSyncRecord(payload, data);
          break;
        case 'createRecord':
          result = await this.handleCreateRecord(payload, data);
          break;
        case 'updateRecord':
          result = await this.handleUpdateRecord(payload, data);
          break;
        // Daily note / line item operations
        case 'getTodayJournal':
          result = await this.handleGetTodayJournal(data);
          break;
        case 'getLineItems':
          result = await this.handleGetLineItems(payload, data);
          break;
        case 'updateLineItem':
          result = await this.handleUpdateLineItem(payload, data);
          break;
        case 'addLineItem':
          result = await this.handleAddLineItem(payload, data);
          break;
        default:
          throw new Error(`Unknown action: ${action}`);
      }
      console.log('[SyncHub] Request success:', action, result);
      this.send(ws, { type: 'response', id, data: result });
    } catch (err) {
      console.error('[SyncHub] Request failed:', action, err.message);
      this.send(ws, { type: 'response', id, error: err.message });
    }
  },

  async handleSyncRecord(payload, data) {
    const { collection, external_id, name, fields } = payload;
    console.log('[SyncHub] syncRecord:', { collection, external_id, name });

    if (!collection) throw new Error('collection required');
    if (!external_id) throw new Error('external_id required');

    // Extract content (markdown body) from fields - handled separately
    const { content, ...recordFields } = fields || {};

    // Find the collection
    const collections = await data.getAllCollections();
    const targetCol = collections.find(
      (c) => c.getName().toLowerCase() === collection.toLowerCase()
    );
    if (!targetCol) throw new Error(`Collection not found: ${collection}`);

    // Find existing record by external_id
    const existingRecords = await targetCol.getAllRecords();
    const existing = existingRecords.find((r) => r.text('external_id') === external_id);
    console.log('[SyncHub] Found existing:', existing ? existing.guid : 'none');

    if (existing) {
      // Update existing record
      this.setRecordFields(existing, { ...recordFields, external_id });

      // Update markdown content if provided
      if (content) {
        console.log('[SyncHub] Replacing markdown content');
        await SyncHubMarkdown.replace(content, existing);
      }

      return { guid: existing.guid, action: 'updated' };
    } else {
      // Create new record
      const guid = targetCol.createRecord(name || 'Untitled');
      if (!guid) throw new Error('Failed to create record');

      // Wait briefly for record to be available
      await new Promise((r) => setTimeout(r, 50));

      const records = await targetCol.getAllRecords();
      const record = records.find((r) => r.guid === guid);
      if (!record) throw new Error('Record created but not found');

      // Set fields including external_id
      this.setRecordFields(record, { ...recordFields, external_id });

      // Insert markdown content for new records
      if (content) {
        console.log('[SyncHub] Inserting markdown content');
        await SyncHubMarkdown.insert(content, record, null);
      }

      return { guid, action: 'created' };
    }
  },

  /**
   * Set fields on a record using the proper Thymer API.
   */
  setRecordFields(record, fields) {
    for (const [fieldId, value] of Object.entries(fields)) {
      if (value === undefined || value === null) continue;
      this.setField(record, fieldId, value);
    }
  },

  /**
   * Set a single field value on a record.
   */
  setField(record, fieldId, value) {
    try {
      const prop = record.prop(fieldId);
      if (!prop) {
        console.warn('[SyncHub] Field not found:', fieldId);
        return;
      }

      if (typeof value === 'string') {
        // Try setChoice first for choice fields (matches by label)
        if (typeof prop.setChoice === 'function') {
          const success = prop.setChoice(value);
          if (!success) {
            prop.set(value);
          }
        } else {
          prop.set(value);
        }
      } else {
        prop.set(value);
      }
    } catch (e) {
      console.warn('[SyncHub] Failed to set field:', fieldId, e.message);
    }
  },

  async handleCreateRecord(payload, data) {
    const { collection, name, fields } = payload;
    const collections = await data.getAllCollections();
    const targetCol = collections.find(
      (c) => c.getName().toLowerCase() === collection.toLowerCase()
    );
    if (!targetCol) throw new Error(`Collection not found: ${collection}`);

    const guid = targetCol.createRecord(name || 'Untitled');
    if (!guid) throw new Error('Failed to create record');

    await new Promise((r) => setTimeout(r, 50));

    const records = await targetCol.getAllRecords();
    const record = records.find((r) => r.guid === guid);
    if (record && fields) {
      const props = record.getAllProperties?.() || [];
      for (const [key, value] of Object.entries(fields)) {
        const prop = props.find((p) => p.name === key || p.id === key);
        if (prop) {
          if (prop.setText) prop.setText(String(value));
          else if (prop.setNumber && typeof value === 'number') prop.setNumber(value);
          else if (prop.setChoice) prop.setChoice(String(value));
        }
      }
    }

    return { guid };
  },

  async handleUpdateRecord(payload, data) {
    const { guid, fields } = payload;
    if (!guid) throw new Error('guid required');

    const record = await SyncHubHelpers.findRecordByGUID(data, guid);
    if (!record) throw new Error(`Record not found: ${guid}`);

    const props = record.getAllProperties?.() || [];
    for (const [key, value] of Object.entries(fields || {})) {
      const prop = props.find((p) => p.name === key || p.id === key);
      if (prop) {
        if (prop.setText) prop.setText(String(value));
        else if (prop.setNumber && typeof value === 'number') prop.setNumber(value);
        else if (prop.setChoice) prop.setChoice(String(value));
      }
    }

    return { guid, success: true };
  },

  async handleInstallPlugin(msg, ctx) {
    const { ws, data, ui } = ctx;
    const { id, config, code, css } = msg;

    try {
      const isCollection = config?.fields || config?.views;
      let pluginAPI = null;

      if (isCollection) {
        const collections = await data.getAllCollections();
        const existing = collections.find((c) => c.getName() === config.name);
        pluginAPI = existing || (await data.createCollection());
      } else {
        const plugins = await data.getAllGlobalPlugins();
        const existing = plugins.find((p) => p.getName() === config.name);
        pluginAPI = existing || (await data.createGlobalPlugin());
      }

      if (!pluginAPI) throw new Error('Failed to get plugin API');

      const success = await pluginAPI.savePlugin(config, code);
      if (!success) throw new Error('savePlugin returned false');

      if (css && pluginAPI.saveCSS) {
        await pluginAPI.saveCSS(css);
      }

      ui.addToaster({
        title: 'Plugin Installed',
        message: `${config.name} v${config.ver || 1}`,
        dismissible: true,
        autoDestroyTime: 3000,
      });
      this.send(ws, { type: 'install_result', id, success: true });
    } catch (err) {
      ui.addToaster({
        title: 'Install Failed',
        message: `${config?.name || id}: ${err.message}`,
        dismissible: true,
        autoDestroyTime: 5000,
      });
      this.send(ws, { type: 'install_result', id, success: false, error: err.message });
    }
  },

  async handleGetInstalledPlugins(msg, ctx) {
    const { ws, data } = ctx;
    const { id } = msg;

    try {
      const plugins = [];

      const collections = await data.getAllCollections();
      for (const col of collections) {
        const name = col.getName();
        if (name === 'Journal') continue;
        const config = col.getConfiguration?.() || {};
        plugins.push({ name, guid: col.guid, type: 'collection', ver: config.ver || 1 });
      }

      const globalPlugins = await data.getAllGlobalPlugins();
      for (const plugin of globalPlugins) {
        const name = plugin.getName();
        const config = plugin.getConfiguration?.() || {};
        plugins.push({ name, guid: plugin.guid, type: 'app', ver: config.ver || 1 });
      }

      this.send(ws, { type: 'installed_plugins', id, data: { plugins } });
    } catch (err) {
      this.send(ws, { type: 'installed_plugins', id, data: { plugins: [], error: err.message } });
    }
  },

  async handleNavigate(msg, ctx) {
    const { target } = msg;
    if (!target) return;

    if (target.startsWith('GUID:')) {
      const guid = target.slice(5);

      // Verify the record exists
      const record = ctx.data?.getRecord(guid);
      if (!record) {
        console.warn('[SyncHub] Navigate: record not found:', guid);
        return;
      }

      // Find a main (non-sidebar) panel to open the record in
      const panels = ctx.ui?.getPanels() || [];
      let mainPanel = panels.find(p => !p.isSidebar());

      // If no main panel exists, create one
      if (!mainPanel) {
        mainPanel = await ctx.ui?.createPanel();
        if (mainPanel) {
          console.log('[SyncHub] Navigate: created new panel');
        }
      }

      if (mainPanel) {
        // Navigate to the record - try different type/id combinations
        // Log current navigation to understand the format
        const currentNav = mainPanel.getNavigation();
        console.log('[SyncHub] Current panel navigation:', JSON.stringify(currentNav));

        mainPanel.navigateTo({
          type: 'document',
          rootId: guid,
          subId: null,
          workspaceGuid: ctx.workspaceGuid,
        });
        console.log('[SyncHub] Navigate: opened record in panel:', guid);
      } else {
        console.warn('[SyncHub] Navigate: could not find or create panel');
      }
    } else if (target.startsWith('http')) {
      window.open(target, '_blank');
    }
  },

  // ===========================================================================
  // Daily Note / Line Item Operations
  // ===========================================================================

  /**
   * Get or create today's daily note (journal entry).
   * Returns the record with its line items (tasks).
   */
  async handleGetTodayJournal(data) {
    // Get the Journal collection
    const collections = await data.getAllCollections();
    const journalCollection = collections.find((c) => c.getName() === 'Journal');
    if (!journalCollection) throw new Error('Journal collection not found');

    const records = await journalCollection.getAllRecords();

    // Journal GUIDs end with YYYYMMDD format (no hyphens)
    const today = new Date().toISOString().slice(0, 10).replace(/-/g, '');
    let todayRecord = records.find((r) => r.guid.endsWith(today));

    // Fallback: Thymer uses prev day until ~3am
    if (!todayRecord) {
      const yesterday = new Date();
      yesterday.setDate(yesterday.getDate() - 1);
      const yesterdayStr = yesterday.toISOString().slice(0, 10).replace(/-/g, '');
      todayRecord = records.find((r) => r.guid.endsWith(yesterdayStr));
    }

    if (!todayRecord) {
      throw new Error('No journal entry found for today');
    }

    // Get line items (with resolved ref titles)
    const lineItems = await this.extractLineItems(todayRecord, data);

    return {
      guid: todayRecord.guid,
      name: todayRecord.getName(),
      date: today,
      line_items: lineItems,
    };
  },

  /**
   * Get line items from a record.
   */
  async handleGetLineItems(payload, data) {
    const { guid } = payload;
    if (!guid) throw new Error('guid required');

    const record = await SyncHubHelpers.findRecordByGUID(data, guid);
    if (!record) throw new Error(`Record not found: ${guid}`);

    const items = await this.extractLineItems(record, data);
    return { items };
  },

  /**
   * Extract line items from a record into a serializable format.
   * Converts segments to markdown text with [title](thymer:uuid) links.
   */
  async extractLineItems(record, data) {
    const lineItems = await record.getLineItems();
    if (!lineItems) return [];

    // Collect all ref GUIDs for batch resolution
    const refGuids = new Set();
    for (const item of lineItems) {
      for (const seg of item.segments || []) {
        if (seg.type === 'ref' && seg.text?.guid) {
          refGuids.add(seg.text.guid);
        }
      }
    }

    // Resolve ref titles (cache for efficiency)
    const refTitles = {};
    if (data && refGuids.size > 0) {
      for (const guid of refGuids) {
        try {
          const refRecord = await SyncHubHelpers.findRecordByGUID(data, guid);
          if (refRecord) {
            refTitles[guid] = refRecord.getName();
          }
        } catch (e) {
          console.warn('[SyncHub] Failed to resolve ref:', guid, e.message);
        }
      }
    }

    return lineItems.map((item) => ({
      guid: item.guid,
      parent_guid: item.parent_guid || null,
      type: item.type || 'bullet',
      checked: item.checked || false,
      // Convert segments to markdown text
      text: this.segmentsToMarkdown(item.segments || [], refTitles),
    }));
  },

  /**
   * Convert segments array to markdown text with thymer: links.
   */
  segmentsToMarkdown(segments, refTitles) {
    let md = '';
    for (const seg of segments) {
      if (seg.type === 'text' && typeof seg.text === 'string') {
        md += seg.text;
      } else if (seg.type === 'ref' && seg.text?.guid) {
        const guid = seg.text.guid;
        const title = refTitles[guid] || 'untitled';
        md += `[${title}](thymer:${guid})`;
      }
    }
    return md.trim();
  },

  /**
   * Update a line item (toggle checked, update text, etc.).
   */
  async handleUpdateLineItem(payload, data) {
    const { record_guid, lineitem_guid, checked, segments } = payload;
    if (!record_guid || !lineitem_guid) throw new Error('record_guid and lineitem_guid required');

    const record = await SyncHubHelpers.findRecordByGUID(data, record_guid);
    if (!record) throw new Error(`Record not found: ${record_guid}`);

    const lineItems = await record.getLineItems();
    const item = lineItems?.find((i) => i.guid === lineitem_guid);
    if (!item) throw new Error(`Line item not found: ${lineitem_guid}`);

    // Update checked state
    if (checked !== undefined && item.setChecked) {
      item.setChecked(checked);
    }

    // Update segments/text
    if (segments !== undefined && item.setSegments) {
      item.setSegments(segments);
    }

    return { guid: lineitem_guid, success: true };
  },

  /**
   * Add a new line item to a record.
   */
  async handleAddLineItem(payload, data) {
    const { record_guid, type, segments, after_guid } = payload;
    if (!record_guid) throw new Error('record_guid required');

    const record = await SyncHubHelpers.findRecordByGUID(data, record_guid);
    if (!record) throw new Error(`Record not found: ${record_guid}`);

    // Find the position to insert after
    let afterItem = null;
    if (after_guid) {
      const lineItems = await record.getLineItems();
      afterItem = lineItems?.find((i) => i.guid === after_guid);
    }

    // Create the line item
    const itemType = type || 'task';
    const newItem = await record.createLineItem(null, afterItem, itemType);
    if (!newItem) throw new Error('Failed to create line item');

    // Set segments if provided
    if (segments && newItem.setSegments) {
      newItem.setSegments(segments);
    }

    return { guid: newItem.guid };
  },
};
