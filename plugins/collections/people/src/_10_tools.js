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
