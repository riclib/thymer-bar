/**
 * Captures Collection Plugin
 *
 * Web pages, highlights, and bookmarks from any source.
 * Sources: Web (llog url), Readwise, Telegram, Kindle, Manual
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    // Wait for SyncHub to register tools
    window.addEventListener('synchub-ready', () => this.registerTools(), { once: true });
    if (window.syncHub) this.registerTools();
  }

  registerTools() {
    if (!window.syncHub?.registerCollectionTools) return;

    window.syncHub.registerCollectionTools({
      collection: 'Captures',
      version: this.getConfiguration().ver || 1,
      description: 'Web pages, highlights, and bookmarks from any source',
      schema: {
        title: 'Page title or highlight text',
        source: 'Web | Readwise | Telegram | Kindle | Manual',
        site: 'Website domain',
        author: 'Author name',
        url: 'Source URL',
        excerpt: 'Page excerpt or highlight',
        captured_at: 'When captured',
        published_at: 'When published',
        tags: 'User tags',
      },
      tools: [
        {
          name: 'find',
          description: 'Find captures by source, author, or site. Returns GUIDs.',
          parameters: {
            source: {
              type: 'string',
              enum: ['Web', 'Readwise', 'Telegram', 'Kindle', 'Manual'],
              optional: true,
            },
            author: { type: 'string', description: 'Author name', optional: true },
            site: { type: 'string', description: 'Website domain', optional: true },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CapturesTools.find(args, data),
        },
        {
          name: 'search',
          description: 'Search captures by text. Returns GUIDs.',
          parameters: {
            query: { type: 'string', description: 'Search text' },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CapturesTools.search(args, data),
        },
        {
          name: 'recent',
          description: 'Get recent captures. Returns GUIDs.',
          parameters: {
            limit: { type: 'number', optional: true },
            source: {
              type: 'string',
              enum: ['Web', 'Readwise', 'Telegram', 'Kindle', 'Manual'],
              optional: true,
            },
          },
          handler: async (args, data) => CapturesTools.recent(args, data),
        },
        {
          name: 'by_site',
          description: 'Get all captures from a specific site. Returns GUIDs.',
          parameters: {
            site: { type: 'string', description: 'Website domain' },
          },
          handler: async (args, data) => CapturesTools.bySite(args, data),
        },
      ],
    });

    console.log('[Captures] Registered collection tools');
  }
}
