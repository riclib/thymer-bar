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
