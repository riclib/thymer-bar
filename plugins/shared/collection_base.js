/**
 * Shared Collection Helpers
 * Common utilities for all collection plugins.
 */

const CollectionBase = {
  /**
   * Get a collection by name.
   */
  async getCollection(data, name) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === name);
  },

  /**
   * Format a record for API response.
   * @param {Object} record - The record to format
   * @param {Array} fields - Field IDs to include
   * @param {Object} formatters - Optional field-specific formatters
   */
  formatRecord(record, fields, formatters = {}) {
    const result = {
      guid: record.guid,
      title: record.getName(),
    };

    for (const fieldId of fields) {
      if (formatters[fieldId]) {
        result[fieldId] = formatters[fieldId](record);
      } else {
        // Auto-detect field type
        const prop = record.prop(fieldId);
        if (!prop) {
          result[fieldId] = record.text(fieldId);
        } else if (prop.choice) {
          result[fieldId] = prop.choice();
        } else if (prop.date) {
          const d = prop.date();
          result[fieldId] = d ? d.toISOString() : null;
        } else if (prop.number) {
          result[fieldId] = prop.number();
        } else if (prop.bool) {
          result[fieldId] = prop.bool();
        } else {
          result[fieldId] = record.text(fieldId);
        }
      }
    }

    return result;
  },

  /**
   * Create choice mapping helpers.
   */
  createChoiceHelpers(choices) {
    const idToLabel = {};
    const labelToId = {};

    for (const [id, label] of Object.entries(choices)) {
      idToLabel[id] = label;
      labelToId[label.toLowerCase()] = id;
    }

    return {
      idToLabel: (id) => idToLabel[id] || id,
      labelToId: (label) => labelToId[label.toLowerCase()] || label.toLowerCase(),
      matches: (record, fieldId, targetLabel) => {
        const choiceId = record.prop(fieldId)?.choice();
        if (!choiceId) return false;
        const targetId = labelToId[targetLabel.toLowerCase()];
        return choiceId.toLowerCase() === targetId?.toLowerCase();
      },
    };
  },

  /**
   * Standard search across text fields.
   */
  searchRecords(records, query, textFields) {
    const queryLower = query.toLowerCase();
    return records.filter((r) => {
      for (const field of textFields) {
        const value = r.text(field)?.toLowerCase() || r.getName()?.toLowerCase() || '';
        if (value.includes(queryLower)) return true;
      }
      return false;
    });
  },

  /**
   * Sort records by a date field.
   */
  sortByDate(records, fieldId, desc = true) {
    return records.sort((a, b) => {
      const dateA = a.prop(fieldId)?.date() || new Date(0);
      const dateB = b.prop(fieldId)?.date() || new Date(0);
      return desc ? dateB - dateA : dateA - dateB;
    });
  },

  /**
   * Create standard collection tools registration.
   */
  registerTools(plugin, config) {
    const { name, version, description, schema, tools } = config;

    window.addEventListener('synchub-ready', () => doRegister(), { once: true });
    if (window.syncHub) doRegister();

    function doRegister() {
      if (!window.syncHub?.registerCollectionTools) return;

      window.syncHub.registerCollectionTools({
        collection: name,
        version,
        description,
        schema,
        tools,
      });

      console.log(`[${name}] Registered collection tools`);
    }
  },
};
