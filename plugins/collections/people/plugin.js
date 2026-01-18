/* People v2 - Generated from src/ - DO NOT EDIT DIRECTLY */
/* Run: make plugins */

// === _00_helpers.js ===
/**
 * People Collection Helpers
 */

const PeopleHelpers = {
  RELATIONSHIPS: {
    contact: 'Contact',
    colleague: 'Colleague',
    friend: 'Friend',
    family: 'Family',
    mentor: 'Mentor',
  },

  labelToId(label) {
    for (const [id, l] of Object.entries(this.RELATIONSHIPS)) {
      if (l.toLowerCase() === label.toLowerCase()) return id;
    }
    return label.toLowerCase();
  },

  idToLabel(id) {
    return this.RELATIONSHIPS[id] || id;
  },

  relationshipMatches(record, targetLabel) {
    const relId = record.prop('relationship')?.choice();
    if (!relId) return false;
    const targetId = this.labelToId(targetLabel);
    return relId.toLowerCase() === targetId.toLowerCase();
  },

  formatPerson(record, includeDetails = false) {
    const result = {
      guid: record.guid,
      name: record.getName(),
      relationship: this.idToLabel(record.prop('relationship')?.choice()),
      company: record.text('company'),
      role: record.text('role'),
      email: record.text('email'),
    };

    if (includeDetails) {
      result.phone = record.text('phone');
      result.location = record.text('location');
      result.linkedin = record.text('linkedin');
      result.twitter = record.text('twitter');
      result.github = record.text('github');
      result.website = record.text('website');
      result.last_contact = record.prop('last_contact')?.date()?.toISOString();
      result.birthday = record.prop('birthday')?.date()?.toISOString();
      result.met_at = record.text('met_at');
    }

    return result;
  },

  daysSinceContact(record) {
    const lastContact = record.prop('last_contact')?.date();
    if (!lastContact) return Infinity;
    const now = new Date();
    const diffMs = now - lastContact;
    return Math.floor(diffMs / (1000 * 60 * 60 * 24));
  },
};

// === _10_tools.js ===
/**
 * People Collection Tools
 */

const PeopleTools = {
  async getCollection(data) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === 'People');
  },

  async find(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'People collection not found' };

    let records = await collection.getAllRecords();

    if (args.relationship) {
      records = records.filter((r) => PeopleHelpers.relationshipMatches(r, args.relationship));
    }
    if (args.company) {
      const companyLower = args.company.toLowerCase();
      records = records.filter((r) =>
        r.text('company')?.toLowerCase().includes(companyLower)
      );
    }
    if (args.location) {
      const locLower = args.location.toLowerCase();
      records = records.filter((r) =>
        r.text('location')?.toLowerCase().includes(locLower)
      );
    }

    const limit = args.limit || 20;
    return records.slice(0, limit).map((r) => PeopleHelpers.formatPerson(r, true));
  },

  async search(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'People collection not found' };

    const records = await collection.getAllRecords();
    const queryLower = args.query.toLowerCase();

    let results = records.filter((r) => {
      const name = r.getName()?.toLowerCase() || '';
      const company = r.text('company')?.toLowerCase() || '';
      const role = r.text('role')?.toLowerCase() || '';
      const email = r.text('email')?.toLowerCase() || '';
      return name.includes(queryLower) || company.includes(queryLower) ||
             role.includes(queryLower) || email.includes(queryLower);
    });

    const limit = args.limit || 10;
    return results.slice(0, limit).map((r) => PeopleHelpers.formatPerson(r, true));
  },

  async byCompany(args, data) {
    if (!args.company) return { error: 'Company required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'People collection not found' };

    const records = await collection.getAllRecords();
    const companyLower = args.company.toLowerCase();

    const results = records.filter((r) =>
      r.text('company')?.toLowerCase().includes(companyLower)
    );

    return {
      company: args.company,
      count: results.length,
      people: results.map((r) => PeopleHelpers.formatPerson(r, true)),
    };
  },

  async needsContact(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'People collection not found' };

    const days = args.days || 30;
    let records = await collection.getAllRecords();

    if (args.relationship) {
      records = records.filter((r) => PeopleHelpers.relationshipMatches(r, args.relationship));
    }

    // Filter to people not contacted in N days
    records = records.filter((r) => PeopleHelpers.daysSinceContact(r) >= days);

    // Sort by days since contact (longest first)
    records.sort((a, b) => {
      return PeopleHelpers.daysSinceContact(b) - PeopleHelpers.daysSinceContact(a);
    });

    const limit = args.limit || 10;
    return records.slice(0, limit).map((r) => ({
      ...PeopleHelpers.formatPerson(r, true),
      days_since_contact: PeopleHelpers.daysSinceContact(r),
    }));
  },

  async recentlyContacted(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'People collection not found' };

    let records = await collection.getAllRecords();

    // Filter to people with last_contact
    records = records.filter((r) => r.prop('last_contact')?.date());

    // Sort by last_contact descending
    records.sort((a, b) => {
      const dateA = a.prop('last_contact')?.date() || new Date(0);
      const dateB = b.prop('last_contact')?.date() || new Date(0);
      return dateB - dateA;
    });

    const limit = args.limit || 10;
    return records.slice(0, limit).map((r) => PeopleHelpers.formatPerson(r, true));
  },
};

// === _99_plugin.js ===
/**
 * People Collection Plugin
 *
 * Your network of contacts, colleagues, and connections.
 * Track relationships, contact history, and connection details.
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    window.addEventListener('synchub-ready', () => this.registerTools(), { once: true });
    if (window.syncHub) this.registerTools();
  }

  registerTools() {
    if (!window.syncHub?.registerCollectionTools) return;

    window.syncHub.registerCollectionTools({
      collection: 'People',
      version: this.getConfiguration().ver || 1,
      description: 'Your network of contacts, colleagues, and connections',
      schema: {
        name: 'Person name',
        relationship: 'Contact | Colleague | Friend | Family | Mentor',
        company: 'Company or organization',
        role: 'Job title or role',
        email: 'Email address',
        phone: 'Phone number',
        location: 'City or region',
        linkedin: 'LinkedIn profile URL',
        twitter: 'Twitter/X profile URL',
        github: 'GitHub profile URL',
        website: 'Personal website',
        last_contact: 'Last contact date',
        birthday: 'Birthday',
        met_at: 'Where/how you met',
        tags: 'User tags',
      },
      tools: [
        {
          name: 'find',
          description: 'Find people by relationship, company, or location. Returns GUIDs.',
          parameters: {
            relationship: {
              type: 'string',
              enum: ['Contact', 'Colleague', 'Friend', 'Family', 'Mentor'],
              optional: true,
            },
            company: { type: 'string', description: 'Company name', optional: true },
            location: { type: 'string', description: 'City or region', optional: true },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => PeopleTools.find(args, data),
        },
        {
          name: 'search',
          description: 'Search people by name, company, role, or email. Returns GUIDs.',
          parameters: {
            query: { type: 'string', description: 'Search text' },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => PeopleTools.search(args, data),
        },
        {
          name: 'by_company',
          description: 'Get all people at a company. Returns GUIDs.',
          parameters: {
            company: { type: 'string', description: 'Company name' },
          },
          handler: async (args, data) => PeopleTools.byCompany(args, data),
        },
        {
          name: 'needs_contact',
          description: 'Find people you haven\'t contacted in N days. Returns GUIDs.',
          parameters: {
            days: { type: 'number', description: 'Days since last contact (default 30)', optional: true },
            relationship: {
              type: 'string',
              enum: ['Contact', 'Colleague', 'Friend', 'Family', 'Mentor'],
              optional: true,
            },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => PeopleTools.needsContact(args, data),
        },
        {
          name: 'recently_contacted',
          description: 'Get recently contacted people. Returns GUIDs.',
          parameters: {
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => PeopleTools.recentlyContacted(args, data),
        },
      ],
    });

    console.log('[People] Registered collection tools');
  }
}

