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
