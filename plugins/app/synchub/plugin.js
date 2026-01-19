/* Sync Hub v46 - Generated from src/ - DO NOT EDIT DIRECTLY */
/* Run: make plugins */

// === _00_helpers.js ===
/**
 * SyncHub Helper Utilities
 * Common helper functions for data access.
 */
const SyncHubHelpers = {
  /**
   * Find a record by GUID across all collections.
   */
  async findRecordByGUID(data, guid) {
    const collections = await data.getAllCollections();
    for (const col of collections) {
      const records = await col.getAllRecords();
      const record = records.find((r) => r.guid === guid);
      if (record) return record;
    }
    return null;
  },

  /**
   * Find a record by external_id field within a specific collection.
   */
  async findRecordByExternalID(data, collectionName, externalId) {
    const collections = await data.getAllCollections();
    const collection = collections.find(
      (c) => c.getName().toLowerCase() === collectionName.toLowerCase()
    );
    if (!collection) return { collection: null, record: null };

    const records = await collection.getAllRecords();
    for (const record of records) {
      const props = record.getAllProperties?.() || [];
      for (const prop of props) {
        if (prop.name === 'external_id' || prop.id === 'external_id') {
          const value = prop.text?.() || prop.choice?.() || null;
          if (value === externalId) {
            return { collection, record };
          }
        }
      }
    }
    return { collection, record: null };
  },

  /**
   * Get today's journal record.
   */
  async getTodayJournal(data) {
    const collections = await data.getAllCollections();
    const journalCollection = collections.find((c) => c.getName() === 'Journal');
    if (!journalCollection) return null;

    const today = new Date().toISOString().slice(0, 10).replace(/-/g, '');
    const records = await journalCollection.getAllRecords();
    return records.find((r) => r.guid.endsWith(today)) || null;
  },

  /**
   * Build OpenAI-style parameters object from simple config.
   */
  buildParameters(params) {
    if (!params) return { type: 'object', properties: {}, required: [] };

    const properties = {};
    const required = [];

    for (const [key, value] of Object.entries(params)) {
      if (typeof value === 'string') {
        const isRequired = !value.endsWith('?');
        properties[key] = { type: value.replace('?', '') };
        if (isRequired) required.push(key);
      } else if (typeof value === 'object') {
        properties[key] = { ...value };
        if (!value.optional) required.push(key);
      }
    }

    return { type: 'object', properties, required };
  },
};

// === _01_markdown.js ===
/**
 * SyncHub Markdown Utilities
 * Parsing and insertion of markdown content into Thymer records.
 */
const SyncHubMarkdown = {
  /**
   * Simple hash function for content comparison.
   */
  hashContent(text) {
    let hash = 0;
    for (let i = 0; i < text.length; i++) {
      hash = (hash << 5) - hash + text.charCodeAt(i);
      hash |= 0;
    }
    return hash.toString(36);
  },

  /**
   * Replace all contents of a record with new markdown.
   * Deletes existing items and inserts fresh content.
   */
  async replace(markdown, record) {
    if (!record) {
      console.warn('[SyncHub] replace: No record provided');
      return 0;
    }

    // Get all existing line items
    const existingItems = await record.getLineItems();

    // Build set of items that have children (so we delete children first)
    const hasChildren = new Set();
    for (const item of existingItems) {
      if (item.parent_guid && item.parent_guid !== record.guid) {
        hasChildren.add(item.parent_guid);
      }
    }

    // Delete leaf items first (items without children)
    for (const item of existingItems) {
      if (!hasChildren.has(item.guid)) {
        try { await item.delete(); } catch (e) { console.warn('[SyncHub] delete failed:', e); }
      }
    }

    // Then delete parent items
    for (const item of existingItems) {
      if (hasChildren.has(item.guid)) {
        try { await item.delete(); } catch (e) { console.warn('[SyncHub] delete failed:', e); }
      }
    }

    // Insert fresh content using explicit object reference
    const count = await SyncHubMarkdown.insert(markdown, record, null);
    console.log('[SyncHub] Replaced content:', count, 'lines');
    return count;
  },

  /**
   * Parse markdown into line data for replaceContents.
   */
  parseMarkdownToLines(markdown) {
    const lines = [];
    let inCode = false, codeLang = '', codeLines = [];
    let isFirstBlock = true;

    for (const line of markdown.split('\n')) {
      const fenceMatch = line.match(/^(\s*)```(.*)$/);
      if (fenceMatch) {
        if (inCode) {
          lines.push({
            type: 'block',
            segments: [{ type: 'text', text: codeLines.join('\n') }],
            lang: codeLang,
          });
          inCode = false;
          codeLang = '';
          codeLines = [];
          isFirstBlock = false;
        } else {
          inCode = true;
          codeLang = fenceMatch[2].trim();
        }
        continue;
      }

      if (inCode) {
        codeLines.push(line);
        continue;
      }

      if (!line.trim()) continue;

      const parsed = this.parseLine(line);
      if (parsed) {
        // Add blank line before headings (except first block)
        if (parsed.type === 'heading' && !isFirstBlock) {
          lines.push({ type: 'text', segments: [] });
        }
        lines.push(parsed);
        isFirstBlock = false;
      }
    }

    return lines;
  },

  /**
   * Insert markdown content into a record after a specific item.
   */
  async insert(markdown, record, afterItem = null) {
    if (!record) return 0;

    let promise = Promise.resolve(afterItem);
    let rendered = 0;
    let inCode = false, codeLang = '', codeLines = [];
    let isFirstBlock = true;

    for (const line of markdown.split('\n')) {
      const fenceMatch = line.match(/^(\s*)```(.*)$/);
      if (fenceMatch) {
        if (inCode) {
          const lang = codeLang, code = [...codeLines];
          promise = promise.then(async (last) => {
            const block = await record.createLineItem(null, last, 'block');
            if (!block) return last;
            try { block.setHighlightLanguage?.(this.normalizeLanguage(lang)); } catch (e) {}
            block.setSegments([]);
            let prev = null;
            for (const cl of code) {
              const li = await record.createLineItem(block, prev, 'text');
              if (li) { li.setSegments([{ type: 'text', text: cl }]); prev = li; }
            }
            rendered++;
            return block;
          });
          isFirstBlock = false;
          inCode = false; codeLang = ''; codeLines = [];
        } else {
          inCode = true; codeLang = fenceMatch[2].trim();
        }
        continue;
      }

      if (inCode) { codeLines.push(line); continue; }
      if (!line.trim()) continue;

      const parsed = this.parseLine(line);
      if (!parsed) continue;

      const { type, segments, level } = parsed;
      const isHeading = type === 'heading';
      const needsBlankLine = isHeading && !isFirstBlock;

      promise = promise.then(async (last) => {
        let insertAfter = last;

        // Add blank line before headings (except first block)
        if (needsBlankLine) {
          const blank = await record.createLineItem(null, insertAfter, 'text');
          if (blank) {
            blank.setSegments([]);
            insertAfter = blank;
          }
        }

        const item = await record.createLineItem(null, insertAfter, type);
        if (!item) return last;
        if (isHeading && level > 1) {
          try { item.setHeadingSize?.(level); } catch (e) {}
        }
        item.setSegments(segments);
        rendered++;
        return item;
      });

      isFirstBlock = false;
    }

    await promise;
    return rendered;
  },

  /**
   * Parse a single line of markdown into type and segments.
   */
  parseLine(line) {
    if (!line.trim()) return null;

    // Horizontal rule
    if (/^(\*\s*\*\s*\*|-\s*-\s*-|_\s*_\s*_)[\s*\-_]*$/.test(line.trim())) {
      return { type: 'br', segments: [] };
    }

    // Headings
    const hMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (hMatch) {
      return {
        type: 'heading',
        level: hMatch[1].length,
        segments: this.parseInline(hMatch[2]),
      };
    }

    // Task list
    const taskMatch = line.match(/^(\s*)[-*]\s+\[([ xX])\]\s+(.+)$/);
    if (taskMatch) {
      return { type: 'task', segments: this.parseInline(taskMatch[3]) };
    }

    // Unordered list
    const ulMatch = line.match(/^(\s*)[-*]\s+(.+)$/);
    if (ulMatch) {
      return { type: 'ulist', segments: this.parseInline(ulMatch[2]) };
    }

    // Ordered list
    const olMatch = line.match(/^(\s*)\d+\.\s+(.+)$/);
    if (olMatch) {
      return { type: 'olist', segments: this.parseInline(olMatch[2]) };
    }

    // Quote
    if (line.startsWith('> ')) {
      return { type: 'quote', segments: this.parseInline(line.slice(2)) };
    }

    // Regular text
    return { type: 'text', segments: this.parseInline(line) };
  },

  /**
   * Parse inline formatting (bold, italic, code, links, refs).
   */
  parseInline(text) {
    const segments = [];
    const patterns = [
      { regex: /`([^`]+)`/, type: 'code' },
      { regex: /\[\[([A-Za-z0-9-]{20,})\]\]/, type: 'ref' },
      { regex: /\[([^\]]+)\]\(([^)]+)\)/, type: 'link' },
      { regex: /\*\*([^*]+)\*\*/, type: 'bold' },
      { regex: /__([^_]+)__/, type: 'bold' },
      { regex: /\*([^*]+)\*/, type: 'italic' },
    ];

    let remaining = text;

    while (remaining.length > 0) {
      let earliestMatch = null;
      let earliestIndex = remaining.length;
      let matchedPattern = null;

      for (const pattern of patterns) {
        const match = remaining.match(pattern.regex);
        if (match && match.index < earliestIndex) {
          earliestMatch = match;
          earliestIndex = match.index;
          matchedPattern = pattern;
        }
      }

      if (earliestMatch && matchedPattern) {
        if (earliestIndex > 0) {
          segments.push({ type: 'text', text: remaining.slice(0, earliestIndex) });
        }

        if (matchedPattern.type === 'link') {
          segments.push({ type: 'text', text: earliestMatch[1] });
        } else if (matchedPattern.type === 'ref') {
          segments.push({ type: 'ref', text: { guid: earliestMatch[1] } });
        } else {
          segments.push({ type: matchedPattern.type, text: earliestMatch[1] });
        }

        remaining = remaining.slice(earliestIndex + earliestMatch[0].length);
      } else {
        segments.push({ type: 'text', text: remaining });
        break;
      }
    }

    return segments.length ? segments : [{ type: 'text', text }];
  },

  /**
   * Normalize language aliases for code blocks.
   */
  normalizeLanguage(lang) {
    if (!lang) return 'plaintext';
    const aliases = {
      js: 'javascript', ts: 'typescript', py: 'python', rb: 'ruby',
      sh: 'bash', yml: 'yaml', 'c++': 'cpp', 'c#': 'csharp', cs: 'csharp',
      golang: 'go', rs: 'rust', kt: 'kotlin', md: 'markdown',
    };
    return aliases[lang.toLowerCase()] || lang.toLowerCase();
  },
};

// === _02_core_tools.js ===
/**
 * SyncHub Core Tools
 * Built-in tools for workspace operations.
 */
const SyncHubCoreTools = {
  /**
   * Get core tool definitions in OpenAI function format.
   */
  getDefinitions() {
    return [
      {
        type: 'function',
        function: {
          name: 'search_workspace',
          description: 'Search across all collections for relevant context.',
          parameters: {
            type: 'object',
            properties: {
              query: { type: 'string', description: 'Search query' },
              limit: { type: 'number', description: 'Max results (default: 5)' },
            },
            required: ['query'],
          },
        },
      },
      {
        type: 'function',
        function: {
          name: 'list_collections',
          description: 'List all available collections and their schemas.',
          parameters: { type: 'object', properties: {}, required: [] },
        },
      },
      {
        type: 'function',
        function: {
          name: 'get_note',
          description: 'Get a note by GUID. Returns title, fields, and body.',
          parameters: {
            type: 'object',
            properties: {
              guid: { type: 'string', description: 'Note GUID' },
            },
            required: ['guid'],
          },
        },
      },
      {
        type: 'function',
        function: {
          name: 'append_to_note',
          description: 'Append markdown content to a note.',
          parameters: {
            type: 'object',
            properties: {
              guid: { type: 'string', description: 'Note GUID' },
              content: { type: 'string', description: 'Markdown content' },
            },
            required: ['guid', 'content'],
          },
        },
      },
      {
        type: 'function',
        function: {
          name: 'log_to_journal',
          description: "Add an entry to today's journal.",
          parameters: {
            type: 'object',
            properties: {
              content: { type: 'string', description: 'Content to add' },
            },
            required: ['content'],
          },
        },
      },
      {
        type: 'function',
        function: {
          name: 'get_todays_journal',
          description: "Get today's journal content.",
          parameters: { type: 'object', properties: {}, required: [] },
        },
      },
      {
        type: 'function',
        function: {
          name: 'save_note',
          description: 'Create a new note in a collection.',
          parameters: {
            type: 'object',
            properties: {
              collection: { type: 'string', description: 'Collection name' },
              content: { type: 'string', description: 'Markdown content' },
            },
            required: ['collection', 'content'],
          },
        },
      },
    ];
  },

  /**
   * Execute a core tool call. Returns null if not a core tool.
   */
  async execute(name, args, ctx) {
    const { data, collectionTools } = ctx;

    switch (name) {
      case 'search_workspace':
        return this.searchWorkspace(args, data);
      case 'list_collections':
        return this.listCollections(data, collectionTools);
      case 'get_note':
        return this.getNote(args, data);
      case 'append_to_note':
        return this.appendToNote(args, data);
      case 'log_to_journal':
        return this.logToJournal(args, data);
      case 'get_todays_journal':
        return this.getTodaysJournal(data);
      case 'save_note':
        return this.saveNote(args, data);
      default:
        return null;
    }
  },

  async searchWorkspace({ query, limit = 5 }, data) {
    try {
      const result = await data.searchByQuery(query, limit);
      return {
        query,
        results: (result.records || []).map((r) => ({
          guid: r.guid,
          title: r.getName?.() || 'Untitled',
          snippet: r.snippet || '',
        })),
      };
    } catch (e) {
      return { error: e.message };
    }
  },

  async listCollections(data, collectionTools) {
    const collections = {};
    const allCollections = await data.getAllCollections();

    for (const col of allCollections) {
      const name = col.getName();
      if (name === 'Sync Hub' || name === 'Journal') continue;

      const registered = collectionTools.get(name);
      collections[name] = {
        guid: col.guid,
        description: registered?.description || `${name} collection`,
        tools: registered ? registered.tools.map((t) => t.name) : [],
      };
    }

    return { collections };
  },

  async getNote({ guid }, data) {
    if (!guid) return { error: 'GUID required' };

    const record = await SyncHubHelpers.findRecordByGUID(data, guid);
    if (!record) return { error: `Note not found: ${guid}` };

    const props = record.getAllProperties?.() || [];
    const fields = {};
    for (const prop of props) {
      const value = prop.choice?.() || prop.text?.() || prop.number?.() || null;
      if (value) fields[prop.name || prop.id] = value;
    }

    const lineItems = (await record.getLineItems?.()) || [];
    const body = lineItems
      .filter((item) => item.parent_guid === record.guid)
      .map((item) =>
        item.segments
          ?.map((s) => (s.type === 'ref' ? `[[${s.text.guid}]]` : s.text || ''))
          .join('') || ''
      )
      .join('\n');

    return {
      guid: record.guid,
      title: record.getName?.() || 'Untitled',
      fields,
      body: body || '(empty)',
    };
  },

  async appendToNote({ guid, content }, data) {
    if (!guid) return { error: 'GUID required' };
    if (!content) return { error: 'Content required' };

    const record = await SyncHubHelpers.findRecordByGUID(data, guid);
    if (!record) return { error: `Note not found: ${guid}` };

    const lineItems = (await record.getLineItems?.()) || [];
    const topLevel = lineItems.filter((item) => item.parent_guid === record.guid);
    const lastItem = topLevel.length > 0 ? topLevel[topLevel.length - 1] : null;

    await SyncHubMarkdown.insert(content, record, lastItem);
    return { success: true, guid };
  },

  async logToJournal({ content }, data) {
    const journal = await SyncHubHelpers.getTodayJournal(data);
    if (!journal) return { error: 'Journal not available' };

    const lineItems = (await journal.getLineItems?.()) || [];
    const topLevel = lineItems.filter((item) => item.parent_guid === journal.guid);
    const lastItem = topLevel.length > 0 ? topLevel[topLevel.length - 1] : null;

    await SyncHubMarkdown.insert(content, journal, lastItem);
    return { success: true };
  },

  async getTodaysJournal(data) {
    const journal = await SyncHubHelpers.getTodayJournal(data);
    if (!journal) {
      return { error: 'No journal found for today.' };
    }

    const lineItems = (await journal.getLineItems?.()) || [];
    const body = lineItems
      .filter((item) => item.parent_guid === journal.guid)
      .map((item) =>
        item.segments
          ?.map((s) => (s.type === 'ref' ? `[[${s.text.guid}]]` : s.text || ''))
          .join('') || ''
      )
      .join('\n');

    return {
      guid: journal.guid,
      title: journal.getName?.() || 'Today',
      date: journal.guid.slice(-8),
      body: body || '(empty)',
    };
  },

  async saveNote({ collection, content }, data) {
    if (!collection) return { error: 'Collection name required' };
    if (!content) return { error: 'Content required' };

    const allCollections = await data.getAllCollections();
    const targetCollection = allCollections.find(
      (c) => c.getName().toLowerCase() === collection.toLowerCase()
    );

    if (!targetCollection) {
      const available = allCollections.map((c) => c.getName()).join(', ');
      return { error: `Collection "${collection}" not found. Available: ${available}` };
    }

    const titleMatch = content.match(/^#\s+(.+)$/m);
    const title = titleMatch ? titleMatch[1].trim() : 'Untitled';

    const guid = targetCollection.createRecord(title);
    if (!guid) return { error: 'Failed to create record' };

    await new Promise((r) => setTimeout(r, 50));

    const records = await targetCollection.getAllRecords();
    const record = records.find((r) => r.guid === guid);
    if (!record) return { error: 'Record created but not found' };

    let bodyContent = content;
    if (titleMatch) {
      bodyContent = content.replace(/^#\s+.+\n?/, '').trim();
    }

    if (bodyContent) {
      await SyncHubMarkdown.insert(bodyContent, record, null);
    }

    return { success: true, guid, title, collection: targetCollection.getName() };
  },
};

// === _03_registry.js ===
/**
 * SyncHub Tool Registry
 * Manages collection tool registration and execution.
 */
const SyncHubRegistry = {
  /**
   * Register tools for a collection.
   */
  register(collectionTools, config, onUpdate) {
    const { collection, description, schema, tools, version } = config;
    if (!collection || !tools) return;

    collectionTools.set(collection, {
      description: description || collection,
      schema: schema || {},
      tools: tools || [],
      version: version || 'unknown',
    });

    if (onUpdate) onUpdate();
  },

  /**
   * Get all registered tools in OpenAI function format.
   */
  getAllTools(collectionTools) {
    const allTools = [];

    // Core tools
    allTools.push(...SyncHubCoreTools.getDefinitions());

    // Collection tools
    for (const [collectionName, config] of collectionTools) {
      for (const tool of config.tools) {
        allTools.push({
          type: 'function',
          function: {
            name: `${collectionName.toLowerCase()}_${tool.name}`,
            description: `[${collectionName}] ${tool.description}`,
            parameters: SyncHubHelpers.buildParameters(tool.parameters),
          },
          _handler: tool.handler,
          _collection: collectionName,
        });
      }
    }

    return allTools;
  },

  /**
   * Execute a tool call by name.
   */
  async execute(name, args, ctx) {
    const { data, ui, collectionTools } = ctx;

    // Try core tools first
    const coreResult = await SyncHubCoreTools.execute(name, args, ctx);
    if (coreResult !== null) {
      return coreResult;
    }

    // Collection tools
    for (const [collectionName, config] of collectionTools) {
      const prefix = collectionName.toLowerCase() + '_';
      if (name.startsWith(prefix)) {
        const toolName = name.slice(prefix.length);
        const tool = config.tools.find((t) => t.name === toolName);
        if (tool?.handler) {
          try {
            return await tool.handler(args, data, ui);
          } catch (e) {
            return { error: e.message };
          }
        }
      }
    }

    return { error: `Unknown tool: ${name}` };
  },
};

// === _04_connect.js ===
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

// === _05_status.js ===
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

// === _06_planner.js ===
/**
 * PlannerHub - Simplified planning sidebar
 *
 * 3 columns: TODAY'S PLAN | DOING | NEXT
 * - Click issue → adds "- [ ] work on [[uuid]]" to daily note
 * - No sorting, no PlannerHub section - brain does the sorting
 */

const PLANNER_CSS = `
    .planner-overlay {
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background: rgba(0, 0, 0, 0.5);
        display: flex;
        align-items: center;
        justify-content: center;
        z-index: 9999;
    }

    .planner-panel {
        background: var(--bg-default, #1e1e1e);
        border-radius: 12px;
        width: 90vw;
        max-width: 1200px;
        height: 80vh;
        display: flex;
        flex-direction: column;
        box-shadow: 0 25px 50px rgba(0, 0, 0, 0.3);
        border: 1px solid var(--border-default, #333);
    }

    .planner-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 16px 20px;
        border-bottom: 1px solid var(--border-default, #333);
        flex-shrink: 0;
    }

    .planner-title {
        display: flex;
        align-items: center;
        gap: 10px;
        font-weight: 600;
        font-size: 15px;
    }

    .planner-title-icon {
        font-size: 18px;
        color: var(--text-muted, #888);
    }

    .planner-date {
        color: var(--text-muted, #888);
        font-weight: 400;
    }

    .planner-close {
        background: none;
        border: none;
        color: var(--text-muted, #888);
        cursor: pointer;
        padding: 8px;
        border-radius: 6px;
        font-size: 18px;
    }

    .planner-close:hover {
        background: var(--bg-hover, #2a2a2a);
        color: var(--text-default, #e0e0e0);
    }

    .planner-kanban {
        display: flex;
        gap: 16px;
        padding: 20px;
        flex: 1;
        overflow: hidden;
    }

    .planner-column {
        flex: 1;
        display: flex;
        flex-direction: column;
        min-width: 0;
        background: var(--bg-subtle, #252525);
        border-radius: 8px;
        overflow: hidden;
    }

    .planner-column-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 12px 16px;
        border-bottom: 1px solid var(--border-default, #333);
        flex-shrink: 0;
    }

    .planner-column-title {
        display: flex;
        align-items: center;
        gap: 8px;
        font-weight: 600;
        font-size: 13px;
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .planner-column-title.plan { color: #10b981; }
    .planner-column-title.doing { color: #f59e0b; }
    .planner-column-title.next { color: #6366f1; }

    .planner-column-count {
        background: var(--bg-hover, #2a2a2a);
        padding: 2px 8px;
        border-radius: 10px;
        font-size: 12px;
        font-weight: 500;
    }

    .planner-column-body {
        flex: 1;
        overflow-y: auto;
        padding: 12px;
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .planner-card {
        background: var(--bg-default, #1e1e1e);
        border: 1px solid var(--border-default, #333);
        border-radius: 6px;
        padding: 12px;
        cursor: pointer;
        transition: all 0.15s ease;
    }

    .planner-card:hover {
        border-color: var(--border-hover, #555);
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
    }

    .planner-card.in-plan {
        opacity: 0.5;
        border-style: dashed;
    }

    .planner-card.done {
        opacity: 0.5;
        text-decoration: line-through;
    }

    .planner-card-header {
        display: flex;
        align-items: flex-start;
        gap: 10px;
    }

    .planner-card-checkbox {
        width: 16px;
        height: 16px;
        border: 2px solid var(--border-default, #333);
        border-radius: 4px;
        flex-shrink: 0;
        margin-top: 2px;
    }

    .planner-card.done .planner-card-checkbox {
        background: var(--text-muted, #888);
        border-color: var(--text-muted, #888);
    }

    .planner-card-title {
        flex: 1;
        font-size: 14px;
        line-height: 1.4;
        word-break: break-word;
    }

    .planner-card-meta {
        margin-top: 8px;
        font-size: 12px;
        color: var(--text-muted, #888);
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .planner-link {
        color: color(display-p3 0.396 0.784 0.733);
    }

    .planner-empty {
        text-align: center;
        padding: 40px 20px;
        color: var(--text-muted, #888);
    }

    .planner-empty-icon {
        font-size: 32px;
        margin-bottom: 8px;
        opacity: 0.5;
    }

    .planner-footer {
        padding: 12px 20px;
        border-top: 1px solid var(--border-default, #333);
        display: flex;
        justify-content: center;
        gap: 24px;
        font-size: 13px;
        color: var(--text-muted, #888);
        flex-shrink: 0;
    }

    .planner-stat {
        display: flex;
        align-items: center;
        gap: 6px;
    }

    .planner-stat-value {
        font-weight: 600;
        color: var(--text-default, #e0e0e0);
    }
`;

const SyncHubPlanner = {
  overlay: null,
  cssInjected: false,

  /**
   * Initialize planner sidebar item
   */
  init(ui, data) {
    this.ui = ui;
    this.data = data;

    // Inject CSS once
    if (!this.cssInjected) {
      ui.injectCSS(PLANNER_CSS);
      this.cssInjected = true;
    }

    // Add sidebar item
    this.sidebarItem = ui.addSidebarItem({
      label: 'Planner',
      icon: 'ti-bow',
      tooltip: 'Daily planning kanban',
      onClick: () => this.show(),
    });

    // Expose API
    window.plannerHub = {
      show: () => this.show(),
      hide: () => this.hide(),
      addToToday: (text, issueGuid) => this.addToToday(text, issueGuid),
      getTodayTasks: () => this.getTodayTasks(),
      getIssues: (status) => this.getIssuesByStatus(status),
    };

    console.log('[PlannerHub] Initialized');
  },

  /**
   * Show the planner overlay
   */
  async show() {
    if (this.overlay) return;

    // Create overlay
    this.overlay = document.createElement('div');
    this.overlay.className = 'planner-overlay';
    this.overlay.addEventListener('click', (e) => {
      if (e.target === this.overlay) this.hide();
    });

    // Create panel
    const panel = document.createElement('div');
    panel.className = 'planner-panel';
    this.overlay.appendChild(panel);

    // Render content
    await this.render(panel);

    document.body.appendChild(this.overlay);
  },

  /**
   * Hide the planner overlay
   */
  hide() {
    if (this.overlay) {
      this.overlay.remove();
      this.overlay = null;
    }
  },

  /**
   * Render the planner panel
   */
  async render(panel) {
    const todayTasks = await this.getTodayTasks();
    const { doing, next } = await this.getAllIssues();

    const todayDate = new Date().toLocaleDateString('en-US', {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
    });

    // GUIDs of issues already in today's plan
    const plannedIssueGuids = new Set(
      todayTasks.filter((t) => t.linkedIssueGuid).map((t) => t.linkedIssueGuid)
    );

    const doneTasks = todayTasks.filter((t) => t.status === 'done').length;

    panel.innerHTML = `
      <div class="planner-header">
        <div class="planner-title">
          <span class="ti ti-calendar-time planner-title-icon"></span>
          <span>Planner</span>
          <span class="planner-date">${todayDate}</span>
        </div>
        <button class="planner-close" data-action="close">
          <span class="ti ti-x"></span>
        </button>
      </div>

      <div class="planner-kanban">
        ${this.renderPlanColumn(todayTasks)}
        ${this.renderIssueColumn('doing', 'Doing', doing, 'ti-progress', plannedIssueGuids)}
        ${this.renderIssueColumn('next', 'Next', next, 'ti-list-check', plannedIssueGuids)}
      </div>

      <div class="planner-footer">
        <div class="planner-stat">
          <span class="planner-stat-value">${todayTasks.length}</span>
          <span>in plan</span>
        </div>
        <div class="planner-stat">
          <span class="planner-stat-value">${doneTasks}</span>
          <span>completed</span>
        </div>
        <div class="planner-stat">
          <span class="planner-stat-value">${doing.length + next.length}</span>
          <span>issues queued</span>
        </div>
      </div>
    `;

    // Wire up actions
    panel.addEventListener('click', async (e) => {
      const action = e.target.closest('[data-action]')?.dataset.action;
      const card = e.target.closest('.planner-card');

      if (action === 'close') {
        this.hide();
        return;
      }

      if (card && card.dataset.type === 'issue' && !card.classList.contains('in-plan')) {
        const issueGuid = card.dataset.guid;
        await this.addToToday(null, issueGuid);
        // Re-render
        await this.render(panel);
      }
    });
  },

  /**
   * Render TODAY'S PLAN column
   */
  renderPlanColumn(tasks) {
    const cardsHtml =
      tasks.length > 0
        ? tasks.map((task) => this.renderTaskCard(task)).join('')
        : `<div class="planner-empty">
             <div class="planner-empty-icon ti ti-checkbox"></div>
             <div>No tasks planned</div>
             <div style="margin-top:8px;font-size:12px">Click issues to add them</div>
           </div>`;

    return `
      <div class="planner-column">
        <div class="planner-column-header">
          <div class="planner-column-title plan">
            <span class="ti ti-calendar-check"></span>
            Today's Plan
          </div>
          <span class="planner-column-count">${tasks.length}</span>
        </div>
        <div class="planner-column-body" data-column="plan">
          ${cardsHtml}
        </div>
      </div>
    `;
  },

  /**
   * Render an issue column (DOING or NEXT)
   */
  renderIssueColumn(id, title, issues, icon, plannedIssueGuids) {
    // Sort: unplanned first, planned at the end
    const sortedIssues = [...issues].sort((a, b) => {
      const aPlanned = plannedIssueGuids.has(a.guid);
      const bPlanned = plannedIssueGuids.has(b.guid);
      if (aPlanned === bPlanned) return 0;
      return aPlanned ? 1 : -1;
    });

    const cardsHtml =
      sortedIssues.length > 0
        ? sortedIssues.map((issue) => this.renderIssueCard(issue, plannedIssueGuids.has(issue.guid))).join('')
        : `<div class="planner-empty">
             <div class="planner-empty-icon ti ti-git-branch"></div>
             <div>No ${title.toLowerCase()} issues</div>
           </div>`;

    return `
      <div class="planner-column">
        <div class="planner-column-header">
          <div class="planner-column-title ${id}">
            <span class="ti ${icon}"></span>
            ${title}
          </div>
          <span class="planner-column-count">${issues.length}</span>
        </div>
        <div class="planner-column-body" data-column="${id}">
          ${cardsHtml}
        </div>
      </div>
    `;
  },

  /**
   * Render a task card (for TODAY'S PLAN)
   */
  renderTaskCard(task) {
    let titleHtml = '';
    if (task.text) {
      titleHtml = this.escapeHtml(task.text);
    }
    if (task.linkedIssueTitle) {
      const linkedHtml = `<span class="planner-link">${this.escapeHtml(task.linkedIssueTitle)}</span>`;
      titleHtml += (titleHtml ? ' ' : '') + linkedHtml;
    }
    if (!titleHtml) {
      titleHtml = '<span style="opacity:0.5">(empty task)</span>';
    }

    const isDone = task.status === 'done';
    const cardClass = `planner-card ${isDone ? 'done' : ''}`;

    return `
      <div class="${cardClass}" data-guid="${task.guid}" data-type="task">
        <div class="planner-card-header">
          <div class="planner-card-checkbox"></div>
          <div class="planner-card-title">${titleHtml}</div>
        </div>
      </div>
    `;
  },

  /**
   * Render an issue card (for DOING/NEXT)
   */
  renderIssueCard(issue, isInPlan) {
    const cardClass = `planner-card ${isInPlan ? 'in-plan' : ''}`;

    return `
      <div class="${cardClass}" data-guid="${issue.guid}" data-type="issue">
        <div class="planner-card-header">
          <div class="planner-card-title">${this.escapeHtml(issue.title)}</div>
        </div>
        <div class="planner-card-meta">
          <span>${issue.source || 'Issue'}</span>
          ${isInPlan ? '<span>• In plan</span>' : ''}
        </div>
      </div>
    `;
  },

  escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  },

  // ===========================================================================
  // Data Access
  // ===========================================================================

  /**
   * Get today's journal record
   */
  async getTodayJournal() {
    const collections = await this.data.getAllCollections();
    const journalCollection = collections.find((c) => c.getName() === 'Journal');
    if (!journalCollection) return null;

    const records = await journalCollection.getAllRecords();
    const today = new Date().toISOString().slice(0, 10).replace(/-/g, '');
    let journal = records.find((r) => r.guid.endsWith(today));

    // Fallback: Thymer uses prev day until ~3am
    if (!journal) {
      const yesterday = new Date();
      yesterday.setDate(yesterday.getDate() - 1);
      const yesterdayStr = yesterday.toISOString().slice(0, 10).replace(/-/g, '');
      journal = records.find((r) => r.guid.endsWith(yesterdayStr));
    }

    return journal;
  },

  /**
   * Get all tasks from today's journal
   */
  async getTodayTasks() {
    const journal = await this.getTodayJournal();
    if (!journal) return [];

    const tasks = [];
    const seenGuids = new Set();

    try {
      const items = await journal.getLineItems();

      for (const item of items) {
        if (item.type !== 'task') continue;
        if (seenGuids.has(item.guid)) continue;
        seenGuids.add(item.guid);

        const segments = item.segments || [];
        let text = '';
        let linkedIssueGuid = null;

        for (const seg of segments) {
          if (seg.type === 'text' && typeof seg.text === 'string') {
            text += seg.text;
          } else if (seg.type === 'ref' && seg.text?.guid) {
            linkedIssueGuid = seg.text.guid;
          }
        }

        // Resolve linked issue title
        let linkedIssueTitle = null;
        if (linkedIssueGuid) {
          const linkedRecord = this.data.getRecord(linkedIssueGuid);
          if (linkedRecord) {
            linkedIssueTitle = linkedRecord.getName();
          }
        }

        const trimmedText = text.trim();
        if (!trimmedText && !linkedIssueGuid) continue;

        // Check status
        const rawStatus = item.props?.done;
        const status = this.getTaskStatus(rawStatus);

        tasks.push({
          guid: item.guid,
          text: trimmedText,
          status,
          linkedIssueGuid,
          linkedIssueTitle,
        });
      }
    } catch (e) {
      console.error('[PlannerHub] Error reading tasks:', e);
    }

    return tasks;
  },

  getTaskStatus(rawDone) {
    if (rawDone === 8) return 'done';
    if (rawDone === 1) return 'in_progress';
    return 'todo';
  },

  /**
   * Get Issues collection
   */
  async getIssuesCollection() {
    const collections = await this.data.getAllCollections();
    return collections.find((c) => c.getName() === 'Issues');
  },

  /**
   * Get issues by status
   */
  async getIssuesByStatus(status) {
    const issuesCollection = await this.getIssuesCollection();
    if (!issuesCollection) return [];

    const records = await issuesCollection.getAllRecords();
    const issues = [];

    for (const record of records) {
      const recordStatus = record.choice('status')?.toLowerCase() || '';
      if (recordStatus === status.toLowerCase()) {
        issues.push({
          guid: record.guid,
          title: record.getName() || '(untitled)',
          source: record.choice('source') || '',
          url: record.text('url'),
        });
      }
    }

    return issues;
  },

  /**
   * Get all issues categorized by status
   */
  async getAllIssues() {
    const issuesCollection = await this.getIssuesCollection();
    if (!issuesCollection) return { doing: [], next: [] };

    const records = await issuesCollection.getAllRecords();
    const doing = [];
    const next = [];

    for (const record of records) {
      const status = record.choice('status')?.toLowerCase() || '';
      const issue = {
        guid: record.guid,
        title: record.getName() || '(untitled)',
        source: record.choice('source') || '',
        url: record.text('url'),
      };

      if (status === 'doing') {
        doing.push(issue);
      } else if (status === 'next') {
        next.push(issue);
      }
    }

    return { doing, next };
  },

  // ===========================================================================
  // Add to Today
  // ===========================================================================

  /**
   * Add a task to today's journal
   * Creates: - [ ] work on [[uuid]]
   */
  async addToToday(text, issueGuid = null) {
    const journal = await this.getTodayJournal();
    if (!journal) {
      console.error('[PlannerHub] No journal found for today');
      return false;
    }

    try {
      // Build segments
      const segments = [];

      if (issueGuid) {
        segments.push({ type: 'text', text: 'work on ' });
        segments.push({ type: 'ref', text: { guid: issueGuid } });
      } else if (text) {
        segments.push({ type: 'text', text: text });
      } else {
        console.error('[PlannerHub] No text or issueGuid provided');
        return false;
      }

      // Get existing items to find insertion point
      const items = await journal.getLineItems();
      const topLevel = items.filter((item) => item.parent_guid === journal.guid);
      const lastItem = topLevel.length > 0 ? topLevel[topLevel.length - 1] : null;

      // Create new task at end of journal (no PlannerHub section)
      const newItem = await journal.createLineItem(null, lastItem, 'task');
      if (newItem) {
        newItem.setSegments(segments);
        console.log('[PlannerHub] Added task:', segments);
        return true;
      } else {
        console.error('[PlannerHub] createLineItem returned null');
        return false;
      }
    } catch (e) {
      console.error('[PlannerHub] Error adding task:', e);
      return false;
    }
  },
};

// === _07_dailynote_watcher.js ===
/**
 * Daily Note Watcher
 *
 * Two-tier polling strategy:
 * 1. Fast poll (5s): Check last item's timestamp + task count
 * 2. Full check (60s): Full fingerprint scan as fallback
 * 3. Deep check (2s debounce): Build and send snapshot after edits stabilize
 *
 * Task status values (props.done):
 * - undefined/0: Unchecked
 * - 1: In Progress
 * - 2: Blocked/Waiting
 * - 8: Done/Completed
 */
const DailyNoteWatcher = {
  // Data API reference for resolving refs
  dataApi: null,

  // Status bar item reference
  statusBarItem: null,

  // Cached journal GUID (avoids search every poll)
  journalGuid: null,
  journalDate: null, // YYYYMMDD - invalidate cache on day change

  // Last known state for change detection
  lastFingerprint: null,      // Full fingerprint (count:maxTimestamp)
  lastTaskGuid: null,         // GUID of last task
  lastTaskTimestamp: null,    // Timestamp of last task
  lastTaskCount: 0,           // Number of tasks

  // Debounce state
  pendingDeepCheck: null,
  lastSeenMaxTimestamp: null,

  // Intervals
  fastPollInterval: null,
  fullCheckInterval: null,
  FAST_POLL_MS: 5000,   // 5 seconds - just check last item
  FULL_CHECK_MS: 60000, // 60 seconds - full fingerprint scan
  DEBOUNCE_MS: 2000,    // 2 seconds

  // Status names for wire format
  statusNames: {
    undefined: 'unchecked',
    0: 'unchecked',
    1: 'in-progress',
    2: 'blocked',
    3: 'cost',
    4: 'important',
    5: 'question',
    6: 'alert',
    7: 'starred',
    8: 'done',
  },

  /**
   * Initialize the watcher with status bar button and polling.
   * Uses global flag to prevent duplicate instances.
   */
  init(ui, data) {
    // Prevent duplicate instances (multiple VMs can load the plugin)
    if (window._dailyNoteWatcherActive) {
      return;
    }
    window._dailyNoteWatcherActive = true;

    this.dataApi = data;

    // Status bar button for manual check
    this.statusBarItem = ui.addStatusBarItem({
      htmlLabel: '<span class="ti ti-refresh synchub-watcher"></span>',
      tooltip: 'Send daily note snapshot',
      onClick: () => this.sendSnapshot(true),
    });

    // Start fast polling
    this.startPolling();

    console.log('[DailyNoteWatcher] Initialized');
  },

  /**
   * Start polling intervals.
   */
  startPolling() {
    // Initial snapshot after short delay
    setTimeout(() => this.sendSnapshot(false), 2000);

    // Fast poll every 5 seconds (just check last item)
    this.fastPollInterval = setInterval(() => this.fastCheck(), this.FAST_POLL_MS);

    // Full check every 60 seconds (full fingerprint scan)
    this.fullCheckInterval = setInterval(() => this.fullCheck(), this.FULL_CHECK_MS);
  },

  /**
   * Clean up on unload.
   */
  cleanup() {
    // Clear global flag so new instance can start
    window._dailyNoteWatcherActive = false;

    if (this.fastPollInterval) {
      clearInterval(this.fastPollInterval);
      this.fastPollInterval = null;
    }
    if (this.fullCheckInterval) {
      clearInterval(this.fullCheckInterval);
      this.fullCheckInterval = null;
    }
    if (this.pendingDeepCheck) {
      clearTimeout(this.pendingDeepCheck);
      this.pendingDeepCheck = null;
    }
    if (this.statusBarItem) {
      this.statusBarItem.remove();
      this.statusBarItem = null;
    }
  },

  /**
   * Fast check - only check the last task's timestamp (sub-ms).
   * Adding new items updates the previous last item's timestamp.
   */
  async fastCheck() {
    // Skip if we don't have a last task to check yet
    if (!this.lastTaskGuid) return;

    const journal = await this.getJournalCached();
    if (!journal) return;

    // Get line items and find our cached last task
    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');

    // Quick checks: count changed or last task missing
    if (tasks.length !== this.lastTaskCount) {
      this.scheduleDeepCheck(Date.now());
      return;
    }

    // Find the cached last task and check its timestamp
    const lastTask = tasks.find(t => t.guid === this.lastTaskGuid);
    if (!lastTask) {
      this.scheduleDeepCheck(Date.now());
      return;
    }

    const currentTimestamp = lastTask.getUpdatedAt?.()?.getTime() || 0;
    if (currentTimestamp !== this.lastTaskTimestamp) {
      this.scheduleDeepCheck(currentTimestamp);
      return;
    }

    // No change detected
  },

  /**
   * Full check - scan all tasks for changes (every 60s fallback).
   */
  async fullCheck() {
    const journal = await this.getJournalCached();
    if (!journal) return;

    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');

    // Build fingerprint: count + max timestamp
    let maxTimestamp = 0;
    for (const task of tasks) {
      const ts = task.getUpdatedAt?.()?.getTime() || 0;
      if (ts > maxTimestamp) maxTimestamp = ts;
    }
    const fingerprint = `${tasks.length}:${maxTimestamp}`;

    // Check if changed since last known
    if (this.lastFingerprint && fingerprint === this.lastFingerprint) {
      return; // No change
    }

    this.scheduleDeepCheck(maxTimestamp);
  },

  /**
   * Schedule a deep check after debounce period.
   */
  scheduleDeepCheck(maxTimestamp) {
    // Clear any pending check
    if (this.pendingDeepCheck) {
      clearTimeout(this.pendingDeepCheck);
    }

    // Remember the timestamp we saw
    this.lastSeenMaxTimestamp = maxTimestamp;

    // Schedule deep check
    this.pendingDeepCheck = setTimeout(() => this.deepCheck(), this.DEBOUNCE_MS);
  },

  /**
   * Deep check - verify timestamps stabilized, then send snapshot.
   */
  async deepCheck() {
    this.pendingDeepCheck = null;

    const journal = await this.getJournalCached();
    if (!journal) return;

    // Check current max timestamp
    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');
    let currentMaxTimestamp = 0;
    for (const task of tasks) {
      const ts = task.getUpdatedAt?.()?.getTime() || 0;
      if (ts > currentMaxTimestamp) currentMaxTimestamp = ts;
    }

    // If still changing, reschedule
    if (currentMaxTimestamp !== this.lastSeenMaxTimestamp) {
      this.scheduleDeepCheck(currentMaxTimestamp);
      return;
    }

    // Timestamp stabilized - send snapshot
    await this.sendSnapshot(false);
  },

  /**
   * Get journal with caching (avoids collection search every poll).
   */
  async getJournalCached() {
    const today = new Date().toISOString().slice(0, 10).replace(/-/g, '');

    // Invalidate cache on day change
    if (this.journalDate !== today) {
      this.journalGuid = null;
      this.journalDate = today;
    }

    // Use syncHub's getTodayJournal (it returns the record)
    const journal = await window.syncHub?.getTodayJournal();
    if (journal) {
      this.journalGuid = journal.guid;
    }
    return journal;
  },

  /**
   * Build and send a full snapshot.
   */
  async sendSnapshot(manual = false) {
    const journal = await window.syncHub?.getTodayJournal();
    if (!journal) return;

    const lineItems = await journal.getLineItems();
    if (!lineItems || lineItems.length === 0) return;

    // Filter for tasks
    const tasks = lineItems.filter(item => item.type === 'task');

    // Build snapshot (includes ref resolution)
    const lines = [];
    for (const task of tasks) {
      const { markdown, refs } = await this.extractMarkdownAndRefs(task);
      const status = task.props?.done;
      const statusName = this.statusNames[status] ?? String(status);
      const updatedAt = task.getUpdatedAt?.();

      // Skip empty tasks
      if (!markdown.trim()) continue;

      lines.push({
        guid: task.guid,
        markdown,
        refs: refs.length > 0 ? refs : undefined,
        status: statusName,
        updatedAt: updatedAt?.toISOString() || new Date().toISOString(),
      });
    }

    // Send to thymer-bar
    if (window.syncHub?.isConnected?.()) {
      const payload = {
        type: 'dailynote.snapshot',
        date: new Date().toISOString().slice(0, 10),
        observedAt: new Date().toISOString(),
        lines,
      };
      window.syncHub.send(payload);
      if (manual) {
        console.log(`[DailyNoteWatcher] Sent ${lines.length} tasks`);
      }
    }

    // Update state for change detection
    let maxTs = 0;
    for (const line of lines) {
      const ts = new Date(line.updatedAt).getTime();
      if (ts > maxTs) maxTs = ts;
    }
    this.lastFingerprint = `${lines.length}:${maxTs}`;
    this.lastTaskCount = lines.length;

    // Cache last task for fast polling
    if (lines.length > 0) {
      const lastLine = lines[lines.length - 1];
      this.lastTaskGuid = lastLine.guid;
      this.lastTaskTimestamp = new Date(lastLine.updatedAt).getTime();
    }
  },

  /**
   * Extract markdown text and resolve ref titles from task segments.
   */
  async extractMarkdownAndRefs(task) {
    const segments = task.segments || [];
    let text = '';
    const refs = [];

    for (const seg of segments) {
      switch (seg.type) {
        case 'text':
          text += seg.text;
          break;
        case 'link':
          text += `[${seg.text}](${seg.text})`;
          break;
        case 'datetime':
          if (seg.text?.d) {
            const d = seg.text.d;
            text += `[${d.slice(0,4)}-${d.slice(4,6)}-${d.slice(6,8)}]`;
          }
          break;
        case 'ref':
          const refGuid = seg.text?.guid;
          if (refGuid) {
            const title = await this.resolveRefTitle(refGuid);
            refs.push({ guid: refGuid, title: title || '(unknown)' });
            text += `[${title || '(unknown)'}](thymer:${refGuid})`;
          }
          break;
        default:
          text += seg.text || '';
      }
    }

    return { markdown: text.trim(), refs };
  },

  /**
   * Resolve a ref GUID to its title.
   */
  async resolveRefTitle(guid) {
    if (!this.dataApi) return null;

    try {
      const record = await SyncHubHelpers.findRecordByGUID(this.dataApi, guid);
      if (record) {
        return record.getName?.() || record.text?.('title') || null;
      }
    } catch (e) {
      // Silently fail
    }
    return null;
  },
};

// === _99_plugin.js ===
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

