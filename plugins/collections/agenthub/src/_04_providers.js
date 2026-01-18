/**
 * AgentHub LLM Providers
 * Streaming implementations for Anthropic, OpenAI, Ollama, and custom endpoints.
 */
const AgentProviders = {
  /**
   * Route to appropriate provider
   */
  async callLLMStreaming(agent, apiKey, systemPrompt, messages, onChunk) {
    const { provider, model, customModel, customEndpoint } = agent;

    switch (provider) {
      case 'openai':
        return this.callOpenAIStreaming(apiKey, model, customModel, customEndpoint, systemPrompt, messages, onChunk);
      case 'ollama':
        return this.callOllamaStreaming(model, customModel, customEndpoint, systemPrompt, messages, onChunk);
      case 'custom':
        return this.callCustomStreaming(apiKey, customModel, customEndpoint, systemPrompt, messages, onChunk);
      case 'anthropic':
      default:
        return this.callAnthropicStreaming(apiKey, model, customModel, systemPrompt, messages, onChunk);
    }
  },

  /**
   * Get tools in Anthropic format
   */
  getToolsForAnthropicAPI() {
    if (!window.syncHub?.getRegisteredTools) return [];
    const tools = window.syncHub.getRegisteredTools();
    return tools.map(t => ({
      name: t.function.name,
      description: t.function.description,
      input_schema: t.function.parameters
    }));
  },

  /**
   * Get tools in OpenAI format
   */
  getToolsForAPI() {
    if (!window.syncHub?.getRegisteredTools) return [];
    const tools = window.syncHub.getRegisteredTools();
    return tools.map(t => ({
      type: t.type,
      function: t.function
    }));
  },

  /**
   * Sanitize messages to remove invalid Unicode surrogates
   */
  sanitizeMessages(messages) {
    return messages.map(m => ({
      ...m,
      content: typeof m.content === 'string'
        ? m.content.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD')
        : m.content
    }));
  },

  // =========================================================================
  // Anthropic
  // =========================================================================

  async callAnthropicStreaming(apiKey, modelChoice, customModel, systemPrompt, messages, onChunk, enableTools = true) {
    const modelMap = {
      'sonnet': 'claude-sonnet-4-5',
      'haiku': 'claude-haiku-4-5',
      'opus': 'claude-opus-4-5',
      'custom': customModel,
    };
    const model = modelMap[modelChoice] || customModel || 'claude-sonnet-4-5';

    const tools = enableTools ? this.getToolsForAnthropicAPI() : null;
    const sanitizedMessages = this.sanitizeMessages(messages);

    const requestBody = {
      model,
      max_tokens: 4096,
      stream: true,
      system: systemPrompt?.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD') || undefined,
      messages: sanitizedMessages,
    };

    if (tools && tools.length > 0) {
      requestBody.tools = tools;
    }

    const response = await fetch('https://api.anthropic.com/v1/messages', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'x-api-key': apiKey,
        'anthropic-version': '2023-06-01',
        'anthropic-dangerous-direct-browser-access': 'true',
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Anthropic API error ${response.status}: ${error}`);
    }

    const result = await this.processAnthropicStream(response, onChunk);

    // If we got tool results, continue the conversation
    if (result?.toolResults && result.toolResults.length > 0) {
      console.log('[AgentHub] Continuing with tool results...');

      const continuationMessages = [
        ...messages,
        {
          role: 'assistant',
          content: result.toolResults.map(tr => ({
            type: 'tool_use',
            id: tr.id,
            name: tr.name,
            input: {}
          }))
        },
        {
          role: 'user',
          content: [
            ...result.toolResults.map(tr => ({
              type: 'tool_result',
              tool_use_id: tr.id,
              content: JSON.stringify(tr.result)
            })),
            {
              type: 'text',
              text: 'Format nicely. Use [[GUID]] for clickable links - they render as the record title, so don\'t repeat the title next to the link.'
            }
          ]
        }
      ];

      const followUpResponse = await fetch('https://api.anthropic.com/v1/messages', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01',
          'anthropic-dangerous-direct-browser-access': 'true',
        },
        body: JSON.stringify({
          model,
          max_tokens: 4096,
          stream: true,
          system: systemPrompt || undefined,
          messages: continuationMessages,
        }),
      });

      if (!followUpResponse.ok) {
        const error = await followUpResponse.text();
        throw new Error(`Anthropic continuation error: ${error}`);
      }

      const finalResult = await this.processAnthropicStream(followUpResponse, (text) => {
        onChunk(result.text + '\n' + text);
      });

      const totalUsage = {
        input_tokens: (result.usage?.input_tokens || 0) + (finalResult.usage?.input_tokens || 0),
        output_tokens: (result.usage?.output_tokens || 0) + (finalResult.usage?.output_tokens || 0)
      };
      return { text: result.text + '\n' + (finalResult.text || finalResult), usage: totalUsage };
    }

    return { text: result.text || result, usage: result.usage || { input_tokens: 0, output_tokens: 0 } };
  },

  async processAnthropicStream(response, onChunk) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let fullText = '';
    let buffer = '';
    let toolUseBlocks = [];
    let currentToolUse = null;
    let usage = { input_tokens: 0, output_tokens: 0 };

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const jsonStr = line.slice(6);
          if (jsonStr === '[DONE]') continue;
          try {
            const data = JSON.parse(jsonStr);

            if (data.type === 'content_block_delta' && data.delta?.text) {
              fullText += data.delta.text;
              onChunk(fullText);
            }

            if (data.type === 'content_block_start' && data.content_block?.type === 'tool_use') {
              currentToolUse = {
                id: data.content_block.id,
                name: data.content_block.name,
                input: ''
              };
            }

            if (data.type === 'content_block_delta' && data.delta?.type === 'input_json_delta') {
              if (currentToolUse) {
                currentToolUse.input += data.delta.partial_json || '';
              }
            }

            if (data.type === 'content_block_stop' && currentToolUse) {
              toolUseBlocks.push(currentToolUse);
              currentToolUse = null;
            }

            if (data.type === 'message_start' && data.message?.usage) {
              usage.input_tokens = data.message.usage.input_tokens || 0;
            }
            if (data.type === 'message_delta' && data.usage) {
              usage.output_tokens = data.usage.output_tokens || 0;
            }
          } catch (e) {}
        }
      }
    }

    // Execute tool calls if any
    if (toolUseBlocks.length > 0) {
      console.log('[AgentHub] Anthropic tool calls detected:', toolUseBlocks);

      const toolResults = [];
      for (const tc of toolUseBlocks) {
        if (tc.name) {
          try {
            const args = tc.input ? JSON.parse(tc.input) : {};
            console.log(`[AgentHub] Executing tool: ${tc.name}`, args);

            fullText += `\n*Using ${tc.name}...*\n`;
            onChunk(fullText);

            const result = await window.syncHub?.executeToolCall(tc.name, args);
            toolResults.push({ id: tc.id, name: tc.name, result: result });
          } catch (e) {
            console.error('[AgentHub] Tool execution error:', e);
            toolResults.push({ id: tc.id, name: tc.name, result: { error: e.message } });
          }
        }
      }

      return { text: fullText, toolResults: toolResults, usage: usage };
    }

    return { text: fullText, usage: usage };
  },

  // =========================================================================
  // OpenAI
  // =========================================================================

  async callOpenAIStreaming(apiKey, modelChoice, customModel, customEndpoint, systemPrompt, messages, onChunk, enableTools = true) {
    const modelMap = {
      'gpt-4o': 'gpt-4o',
      'gpt-4o-mini': 'gpt-4o-mini',
      'custom': customModel,
    };
    const model = modelMap[modelChoice] || customModel || 'gpt-4o';
    const endpoint = customEndpoint || 'https://api.openai.com/v1/chat/completions';

    const openaiMessages = systemPrompt
      ? [{ role: 'system', content: systemPrompt }, ...messages]
      : messages;

    const tools = enableTools ? this.getToolsForAPI() : null;

    const requestBody = {
      model,
      max_tokens: 4096,
      stream: true,
      stream_options: { include_usage: true },
      messages: openaiMessages,
    };

    if (tools && tools.length > 0) {
      requestBody.tools = tools;
      requestBody.tool_choice = 'auto';
    }

    const response = await fetch(endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`,
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`OpenAI API error ${response.status}: ${error}`);
    }

    const result = await this.processOpenAIStream(response, onChunk);

    // Handle tool calls
    if (result?.toolCalls && result.toolCalls.length > 0) {
      let fullText = result.text || '';
      const toolResults = [];

      for (const tc of result.toolCalls) {
        try {
          const args = tc.arguments ? JSON.parse(tc.arguments) : {};
          fullText += `\n*Using ${tc.name}...*\n`;
          onChunk(fullText);

          const toolResult = await window.syncHub?.executeToolCall(tc.name, args);
          toolResults.push({ id: tc.id, name: tc.name, result: toolResult });
        } catch (e) {
          toolResults.push({ id: tc.id, name: tc.name, result: { error: e.message } });
        }
      }

      const continuationMessages = [
        ...openaiMessages,
        {
          role: 'assistant',
          content: '',
          tool_calls: result.toolCalls.map(tc => ({
            id: tc.id,
            type: 'function',
            function: { name: tc.name, arguments: tc.arguments || '{}' }
          }))
        },
        ...toolResults.map(tr => ({
          role: 'tool',
          tool_call_id: tr.id,
          content: JSON.stringify(tr.result)
        })),
        { role: 'user', content: 'Format nicely. Use [[GUID]] for clickable links.' }
      ];

      const followUpResponse = await fetch(endpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          model,
          max_tokens: 4096,
          stream: true,
          messages: continuationMessages,
        }),
      });

      if (followUpResponse.ok) {
        const finalResult = await this.processOpenAIStream(followUpResponse, (text) => {
          onChunk(fullText + '\n' + text);
        });
        const totalUsage = {
          input_tokens: (result.usage?.input_tokens || 0) + (finalResult.usage?.input_tokens || 0),
          output_tokens: (result.usage?.output_tokens || 0) + (finalResult.usage?.output_tokens || 0)
        };
        return { text: fullText + '\n' + (finalResult?.text || finalResult), usage: totalUsage };
      }

      return { text: fullText, usage: result.usage || { input_tokens: 0, output_tokens: 0 } };
    }

    return { text: result?.text || result, usage: result?.usage || { input_tokens: 0, output_tokens: 0 } };
  },

  async processOpenAIStream(response, onChunk) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let fullText = '';
    let buffer = '';
    let toolCalls = [];
    let usage = { input_tokens: 0, output_tokens: 0 };

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const jsonStr = line.slice(6);
          if (jsonStr === '[DONE]') continue;
          try {
            const data = JSON.parse(jsonStr);
            const choice = data.choices?.[0];

            const delta = choice?.delta?.content;
            if (delta) {
              fullText += delta;
              onChunk(fullText);
            }

            const toolCallDelta = choice?.delta?.tool_calls;
            if (toolCallDelta) {
              for (const tc of toolCallDelta) {
                const idx = tc.index ?? toolCalls.length;
                if (!toolCalls[idx]) {
                  toolCalls[idx] = { id: tc.id || `call_${idx}`, name: tc.function?.name || '', arguments: '' };
                }
                if (tc.id) toolCalls[idx].id = tc.id;
                if (tc.function?.name) toolCalls[idx].name = tc.function.name;
                if (tc.function?.arguments) {
                  const args = tc.function.arguments;
                  toolCalls[idx].arguments += typeof args === 'string' ? args : JSON.stringify(args);
                }
              }
            }

            if (data.usage) {
              usage.input_tokens = data.usage.prompt_tokens || 0;
              usage.output_tokens = data.usage.completion_tokens || 0;
            }
          } catch (e) {}
        }
      }
    }

    if (toolCalls.length > 0) {
      return { text: fullText, toolCalls, usage };
    }
    return { text: fullText, usage };
  },

  // =========================================================================
  // Ollama
  // =========================================================================

  async callOllamaStreaming(modelChoice, customModel, customEndpoint, systemPrompt, messages, onChunk, enableTools = true) {
    const model = customModel || modelChoice || 'llama3.2';
    const endpoint = customEndpoint || 'http://localhost:11434/api/chat';

    if (window.location.protocol === 'https:' && endpoint.startsWith('http://')) {
      throw new Error(`Mixed content blocked: Cannot call HTTP endpoint from HTTPS page.`);
    }

    const ollamaMessages = systemPrompt
      ? [{ role: 'system', content: systemPrompt }, ...messages]
      : messages;

    const tools = enableTools ? this.getToolsForAPI() : null;

    const requestBody = { model, stream: true, messages: ollamaMessages };
    if (tools && tools.length > 0) {
      requestBody.tools = tools;
    }

    let response;
    try {
      response = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });
    } catch (fetchError) {
      throw new Error(`Failed to connect to Ollama at ${endpoint}: ${fetchError.message}`);
    }

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Ollama API error ${response.status}: ${error}`);
    }

    const result = await this.processOllamaStream(response, onChunk);

    // Handle tool calls similar to OpenAI
    if (result?.toolCalls && result.toolCalls.length > 0) {
      const toolResults = [];
      for (const tc of result.toolCalls) {
        try {
          const args = tc.arguments ? JSON.parse(tc.arguments) : {};
          result.text += `\n*Using ${tc.name}...*\n`;
          onChunk(result.text);

          const toolResult = await window.syncHub?.executeToolCall(tc.name, args);
          toolResults.push({ name: tc.name, result: toolResult });
        } catch (e) {
          toolResults.push({ name: tc.name, result: { error: e.message } });
        }
      }

      const continuationMessages = [
        ...ollamaMessages,
        { role: 'assistant', content: '', tool_calls: result.toolCalls.map((tc, i) => ({
          id: `call_${i}`,
          type: 'function',
          function: { name: tc.name, arguments: tc.arguments || '{}' }
        }))},
        ...toolResults.map((tr, i) => ({
          role: 'tool',
          tool_call_id: `call_${i}`,
          content: JSON.stringify(tr.result)
        })),
        { role: 'user', content: 'Format nicely. Use [[GUID]] for clickable links.' }
      ];

      const followUpResponse = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model, stream: true, messages: continuationMessages }),
      });

      if (followUpResponse.ok) {
        const finalResult = await this.processOllamaStream(followUpResponse, (text) => {
          onChunk(result.text + '\n' + text);
        });
        const totalUsage = {
          input_tokens: (result.usage?.input_tokens || 0) + (finalResult.usage?.input_tokens || 0),
          output_tokens: (result.usage?.output_tokens || 0) + (finalResult.usage?.output_tokens || 0)
        };
        return { text: result.text + '\n' + (finalResult?.text || finalResult), usage: totalUsage };
      }

      return { text: result.text, usage: result.usage || { input_tokens: 0, output_tokens: 0 } };
    }

    return { text: result?.text || result, usage: result?.usage || { input_tokens: 0, output_tokens: 0 } };
  },

  async processOllamaStream(response, onChunk) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let fullText = '';
    let buffer = '';
    let toolCalls = [];
    let usage = { input_tokens: 0, output_tokens: 0 };

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      for (const line of lines) {
        if (!line.trim()) continue;
        try {
          const data = JSON.parse(line);

          if (data.message?.content) {
            fullText += data.message.content;
            onChunk(fullText);
          }

          if (data.message?.tool_calls) {
            for (const tc of data.message.tool_calls) {
              toolCalls.push({
                name: tc.function?.name,
                arguments: JSON.stringify(tc.function?.arguments || {})
              });
            }
          }

          if (data.done && data.prompt_eval_count !== undefined) {
            usage.input_tokens = data.prompt_eval_count || 0;
            usage.output_tokens = data.eval_count || 0;
          }
        } catch (e) {}
      }
    }

    if (toolCalls.length > 0) {
      return { text: fullText, toolCalls, usage };
    }
    return { text: fullText, usage };
  },

  // =========================================================================
  // Custom (OpenAI-compatible)
  // =========================================================================

  async callCustomStreaming(apiKey, customModel, customEndpoint, systemPrompt, messages, onChunk, enableTools = true) {
    if (!customEndpoint) throw new Error('Custom endpoint required');

    const customMessages = systemPrompt
      ? [{ role: 'system', content: systemPrompt }, ...messages]
      : messages;

    const headers = { 'Content-Type': 'application/json' };
    if (apiKey) headers['Authorization'] = `Bearer ${apiKey}`;

    const tools = enableTools ? this.getToolsForAPI() : null;

    const requestBody = {
      model: customModel || 'default',
      stream: true,
      stream_options: { include_usage: true },
      messages: customMessages,
    };

    if (tools && tools.length > 0) {
      requestBody.tools = tools;
    }

    const response = await fetch(customEndpoint, {
      method: 'POST',
      headers,
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Custom API error ${response.status}: ${error}`);
    }

    // Use OpenAI stream processor (most custom endpoints are OpenAI-compatible)
    const result = await this.processOpenAIStream(response, onChunk);

    // Handle tool calls same as OpenAI
    if (result?.toolCalls && result.toolCalls.length > 0) {
      let fullText = result.text || '';
      const toolResults = [];

      for (const tc of result.toolCalls) {
        try {
          const args = tc.arguments ? JSON.parse(tc.arguments) : {};
          fullText += `\n*Using ${tc.name}...*\n`;
          onChunk(fullText);

          const toolResult = await window.syncHub?.executeToolCall(tc.name, args);
          toolResults.push({ id: tc.id, name: tc.name, result: toolResult });
        } catch (e) {
          toolResults.push({ id: tc.id, name: tc.name, result: { error: e.message } });
        }
      }

      const continuationMessages = [
        ...customMessages,
        {
          role: 'assistant',
          content: '',
          tool_calls: result.toolCalls.map(tc => ({
            id: tc.id,
            type: 'function',
            function: { name: tc.name, arguments: tc.arguments || '{}' }
          }))
        },
        ...toolResults.map(tr => ({
          role: 'tool',
          tool_call_id: tr.id,
          content: JSON.stringify(tr.result)
        })),
        { role: 'user', content: 'Format nicely. Use [[GUID]] for clickable links.' }
      ];

      const followUpResponse = await fetch(customEndpoint, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: customModel || 'default',
          stream: true,
          messages: continuationMessages,
        }),
      });

      if (followUpResponse.ok) {
        const finalResult = await this.processOpenAIStream(followUpResponse, (text) => {
          onChunk(fullText + '\n' + text);
        });
        const totalUsage = {
          input_tokens: (result.usage?.input_tokens || 0) + (finalResult.usage?.input_tokens || 0),
          output_tokens: (result.usage?.output_tokens || 0) + (finalResult.usage?.output_tokens || 0)
        };
        return { text: fullText + '\n' + (finalResult?.text || finalResult), usage: totalUsage };
      }

      return { text: fullText, usage: result.usage || { input_tokens: 0, output_tokens: 0 } };
    }

    return { text: result?.text || result, usage: result?.usage || { input_tokens: 0, output_tokens: 0 } };
  },

  // =========================================================================
  // Quick completion (for title suggestions)
  // =========================================================================

  async quickCompletion(agent, apiKey, prompt) {
    const { provider, model, customModel, customEndpoint } = agent;

    const messages = [{ role: 'user', content: prompt }];

    let endpoint, headers, body;

    if (provider === 'anthropic') {
      endpoint = 'https://api.anthropic.com/v1/messages';
      headers = {
        'Content-Type': 'application/json',
        'x-api-key': apiKey,
        'anthropic-version': '2023-06-01',
        'anthropic-dangerous-direct-browser-access': 'true',
      };
      body = {
        model: model === 'haiku' ? 'claude-3-haiku-20240307' : 'claude-sonnet-4-20250514',
        max_tokens: 50,
        messages,
      };
    } else if (provider === 'custom' || provider === 'ollama') {
      endpoint = customEndpoint || 'http://localhost:11434/api/chat';
      headers = { 'Content-Type': 'application/json' };
      if (apiKey) headers['Authorization'] = `Bearer ${apiKey}`;
      body = {
        model: customModel || 'default',
        max_tokens: 50,
        messages,
      };
    } else {
      endpoint = customEndpoint || 'https://api.openai.com/v1/chat/completions';
      headers = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`,
      };
      body = {
        model: customModel || 'gpt-4o-mini',
        max_tokens: 50,
        messages,
      };
    }

    const response = await fetch(endpoint, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    });

    if (!response.ok) return null;

    const data = await response.json();

    if (provider === 'anthropic') {
      return data.content?.[0]?.text || null;
    } else {
      return data.choices?.[0]?.message?.content || null;
    }
  }
};
