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
