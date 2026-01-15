/**
 * Captures Collection Tools
 */

const CapturesTools = {
  async getCollection(data) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === 'Captures');
  },

  async find(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    let records = await collection.getAllRecords();

    if (args.source) {
      records = records.filter((r) => CapturesHelpers.sourceMatches(r, args.source));
    }
    if (args.author) {
      const authorLower = args.author.toLowerCase();
      records = records.filter((r) =>
        r.text('author')?.toLowerCase().includes(authorLower)
      );
    }
    if (args.site) {
      const siteLower = args.site.toLowerCase();
      records = records.filter((r) =>
        r.text('site')?.toLowerCase().includes(siteLower)
      );
    }

    const limit = args.limit || 20;
    return records.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r));
  },

  async search(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    const records = await collection.getAllRecords();
    const queryLower = args.query.toLowerCase();

    let results = records.filter((r) => {
      const title = r.getName()?.toLowerCase() || '';
      const excerpt = r.text('excerpt')?.toLowerCase() || '';
      const site = r.text('site')?.toLowerCase() || '';
      return title.includes(queryLower) || excerpt.includes(queryLower) || site.includes(queryLower);
    });

    const limit = args.limit || 10;
    return results.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r, true));
  },

  async recent(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    let records = await collection.getAllRecords();

    if (args.source) {
      records = records.filter((r) => CapturesHelpers.sourceMatches(r, args.source));
    }

    // Sort by captured_at descending
    records.sort((a, b) => {
      const dateA = a.prop('captured_at')?.date() || new Date(0);
      const dateB = b.prop('captured_at')?.date() || new Date(0);
      return dateB - dateA;
    });

    const limit = args.limit || 10;
    return records.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r, true));
  },

  async bySite(args, data) {
    if (!args.site) return { error: 'Site required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    const records = await collection.getAllRecords();
    const siteLower = args.site.toLowerCase();

    const results = records.filter((r) =>
      r.text('site')?.toLowerCase().includes(siteLower)
    );

    return {
      site: args.site,
      count: results.length,
      captures: results.map((r) => CapturesHelpers.formatCapture(r, true)),
    };
  },
};
