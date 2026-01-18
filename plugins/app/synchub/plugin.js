/* Sync Hub v23 - Generated from src/ - DO NOT EDIT DIRECTLY */
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
          this.handleNavigate(msg, ui);
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

  handleNavigate(msg, ui) {
    const { target } = msg;
    if (!target) return;

    if (target.startsWith('GUID:')) {
      ui.getActivePanel()?.openRecord(target.slice(5));
    } else if (target.startsWith('http')) {
      window.open(target, '_blank');
    }
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
  }

  onUnload() {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    if (this.statusBarItem) {
      this.statusBarItem.remove();
    }
    if (this.pasteCommand) {
      this.pasteCommand.remove();
    }
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
    }
    delete window.syncHub;
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
    SyncHubConnect.handleMessage(data, {
      ws: this.ws,
      data: this.data,
      ui: this.ui,
      collectionTools: this.collectionTools,
      sendToolsManifest: () => this.sendToolsManifest(),
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

