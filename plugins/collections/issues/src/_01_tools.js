/**
 * Issues Tool Handlers
 * MCP tool implementations for querying issues.
 */
const IssuesTools = {
  /**
   * Get tool definitions for registration.
   */
  getDefinitions(version) {
    return {
      collection: 'Issues',
      version: `v${version}`,
      description: 'Issues and pull requests from any source (GitHub, GitLab, Jira, etc.)',
      schema: {
        title: 'Issue title',
        status: 'Inbox | Backlog | Next | Doing | Done | Cancelled',
        type: 'Issue | PR | Task | Bug | Feature',
        repo: 'Repository name (owner/repo)',
        project: 'Project grouping',
        assignee: 'Assigned user',
        author: 'Issue author',
        number: 'Issue number',
        url: 'Link to issue',
      },
      tools: [
        {
          name: 'find',
          description:
            'Find issues by status, repo, type, or assignee. Returns GUIDs - use [[GUID]] in your response to create clickable links.',
          parameters: {
            status: {
              type: 'string',
              enum: ['Inbox', 'Backlog', 'Next', 'Doing', 'Done', 'Cancelled'],
              optional: true,
            },
            type: {
              type: 'string',
              enum: ['Issue', 'PR', 'Task', 'Bug', 'Feature'],
              optional: true,
            },
            repo: { type: 'string', description: 'Repository name (e.g. owner/repo)', optional: true },
            assignee: { type: 'string', optional: true },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => IssuesTools.find(args, data),
        },
        {
          name: 'get',
          description: 'Get full details of an issue by number or title. Returns GUID - use [[GUID]] to link.',
          parameters: { query: 'string' },
          handler: async (args, data) => IssuesTools.get(args, data),
        },
        {
          name: 'search',
          description: 'Search issues by text in title or content. Returns GUIDs - use [[GUID]] to link.',
          parameters: {
            query: { type: 'string', description: 'Search text' },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => IssuesTools.search(args, data),
        },
        {
          name: 'summarize_open',
          description:
            'Get summary of all open issues. Returns GUIDs - use [[GUID]] to create clickable links to each issue.',
          parameters: {
            repo: { type: 'string', optional: true },
            project: { type: 'string', optional: true },
          },
          handler: async (args, data) => IssuesTools.summarizeOpen(args, data),
        },
      ],
    };
  },

  /**
   * Get the Issues collection.
   */
  async getCollection(data) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === 'Issues');
  },

  /**
   * Find issues by filters.
   */
  async find(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Issues collection not found' };

    const records = await collection.getAllRecords();
    let results = records;

    if (args.status) {
      results = results.filter((r) => IssuesState.matches(r, args.status));
    }
    if (args.type) {
      const typeId = args.type.toLowerCase().replace(/ /g, '_').replace('pr', 'pull_request');
      results = results.filter((r) => {
        const recordTypeId = r.prop('type')?.choice();
        return recordTypeId === typeId || recordTypeId?.toLowerCase() === args.type.toLowerCase();
      });
    }
    if (args.repo) {
      const repoLower = args.repo.toLowerCase();
      results = results.filter((r) => r.text('repo')?.toLowerCase().includes(repoLower));
    }
    if (args.assignee) {
      const assigneeLower = args.assignee.toLowerCase();
      results = results.filter((r) => r.text('assignee')?.toLowerCase().includes(assigneeLower));
    }

    const limit = args.limit || 20;
    results = results.slice(0, limit);

    return results.map((r) => ({
      guid: r.guid,
      title: r.getName(),
      status: IssuesState.idToLabel(r.prop('status')?.choice(), 'state'),
      type: IssuesState.idToLabel(r.prop('type')?.choice(), 'type'),
      repo: r.text('repo'),
      number: r.prop('number')?.number(),
      assignee: r.text('assignee'),
    }));
  },

  /**
   * Get issue by number or title.
   */
  async get(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Issues collection not found' };

    const records = await collection.getAllRecords();
    const query = args.query.toLowerCase();

    // Try to match by number first
    const numberMatch = query.match(/^#?(\d+)$/);
    if (numberMatch) {
      const num = parseInt(numberMatch[1], 10);
      const found = records.find((r) => r.prop('number')?.number() === num);
      if (found) {
        return {
          guid: found.guid,
          title: found.getName(),
          status: IssuesState.idToLabel(found.prop('status')?.choice(), 'state'),
          type: IssuesState.idToLabel(found.prop('type')?.choice(), 'type'),
          repo: found.text('repo'),
          number: found.prop('number')?.number(),
          url: found.text('url'),
        };
      }
    }

    // Fall back to title search
    const found = records.find((r) => r.getName()?.toLowerCase().includes(query));
    if (found) {
      return {
        guid: found.guid,
        title: found.getName(),
        status: IssuesState.idToLabel(found.prop('status')?.choice(), 'state'),
        type: IssuesState.idToLabel(found.prop('type')?.choice(), 'type'),
        repo: found.text('repo'),
        number: found.prop('number')?.number(),
        url: found.text('url'),
      };
    }

    return { error: 'Issue not found' };
  },

  /**
   * Search issues by text.
   */
  async search(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Issues collection not found' };

    const records = await collection.getAllRecords();
    const queryLower = args.query.toLowerCase();

    let results = records.filter((r) => {
      const title = r.getName()?.toLowerCase() || '';
      const repo = r.text('repo')?.toLowerCase() || '';
      return title.includes(queryLower) || repo.includes(queryLower);
    });

    const limit = args.limit || 10;
    results = results.slice(0, limit);

    return results.map((r) => ({
      guid: r.guid,
      title: r.getName(),
      status: IssuesState.idToLabel(r.prop('status')?.choice(), 'state'),
      type: IssuesState.idToLabel(r.prop('type')?.choice(), 'type'),
      repo: r.text('repo'),
    }));
  },

  /**
   * Summarize open issues.
   */
  async summarizeOpen(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Issues collection not found' };

    const records = await collection.getAllRecords();

    // Filter to open states
    let results = records.filter((r) => IssuesState.isOpen(r.prop('status')?.choice()));

    if (args.repo) {
      const repoLower = args.repo.toLowerCase();
      results = results.filter((r) => r.text('repo')?.toLowerCase().includes(repoLower));
    }
    if (args.project) {
      const projectId = args.project.toLowerCase().replace(/ /g, '_');
      results = results.filter((r) => {
        const recordProjectId = r.prop('project')?.choice();
        return (
          recordProjectId === projectId || recordProjectId?.toLowerCase().includes(args.project.toLowerCase())
        );
      });
    }

    // Group by status
    const byStatus = {};
    for (const r of results) {
      const statusId = r.prop('status')?.choice();
      const statusLabel = IssuesState.idToLabel(statusId, 'state') || 'Unknown';
      if (!byStatus[statusLabel]) byStatus[statusLabel] = [];
      byStatus[statusLabel].push({
        guid: r.guid,
        title: r.getName(),
        repo: r.text('repo'),
        type: IssuesState.idToLabel(r.prop('type')?.choice(), 'type'),
      });
    }

    return {
      total: results.length,
      by_status: byStatus,
    };
  },
};
