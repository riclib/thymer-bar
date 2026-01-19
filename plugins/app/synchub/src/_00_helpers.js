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

    // Use local date, not UTC (toISOString returns UTC)
    const now = new Date();
    const today = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`;
    const records = await journalCollection.getAllRecords();
    let journal = records.find((r) => r.guid.endsWith(today));

    // Fallback: Thymer uses prev day until ~3am (for UI/PlannerHub)
    if (!journal) {
      const yesterday = new Date();
      yesterday.setDate(yesterday.getDate() - 1);
      const yesterdayStr = `${yesterday.getFullYear()}${String(yesterday.getMonth() + 1).padStart(2, '0')}${String(yesterday.getDate()).padStart(2, '0')}`;
      journal = records.find((r) => r.guid.endsWith(yesterdayStr));
    }

    return journal;
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
