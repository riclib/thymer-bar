/**
 * SyncHub Tool Registry
 * Manages collection tool registration and execution.
 */
const SyncHubRegistry = {
  /**
   * Register tools for a collection.
   */
  register(collectionTools, config, onUpdate) {
    const { collection, description, schema, tools, version } = config;
    if (!collection || !tools) return;

    collectionTools.set(collection, {
      description: description || collection,
      schema: schema || {},
      tools: tools || [],
      version: version || 'unknown',
    });

    if (onUpdate) onUpdate();
  },

  /**
   * Get all registered tools in OpenAI function format.
   */
  getAllTools(collectionTools) {
    const allTools = [];

    // Core tools
    allTools.push(...SyncHubCoreTools.getDefinitions());

    // Collection tools
    for (const [collectionName, config] of collectionTools) {
      for (const tool of config.tools) {
        allTools.push({
          type: 'function',
          function: {
            name: `${collectionName.toLowerCase()}_${tool.name}`,
            description: `[${collectionName}] ${tool.description}`,
            parameters: SyncHubHelpers.buildParameters(tool.parameters),
          },
          _handler: tool.handler,
          _collection: collectionName,
        });
      }
    }

    return allTools;
  },

  /**
   * Execute a tool call by name.
   */
  async execute(name, args, ctx) {
    const { data, ui, collectionTools } = ctx;

    // Try core tools first
    const coreResult = await SyncHubCoreTools.execute(name, args, ctx);
    if (coreResult !== null) {
      return coreResult;
    }

    // Collection tools
    for (const [collectionName, config] of collectionTools) {
      const prefix = collectionName.toLowerCase() + '_';
      if (name.startsWith(prefix)) {
        const toolName = name.slice(prefix.length);
        const tool = config.tools.find((t) => t.name === toolName);
        if (tool?.handler) {
          try {
            return await tool.handler(args, data, ui);
          } catch (e) {
            return { error: e.message };
          }
        }
      }
    }

    return { error: `Unknown tool: ${name}` };
  },
};
