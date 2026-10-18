#!/usr/bin/env node
// Calls Gemini CLI's bundled source loader directly. This does not start a CLI
// session, authenticate, invoke an agent, or call a model.
import fs from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const [bundleDir, positiveDir, malformedDir, missingDir] = process.argv.slice(2);
if (![bundleDir, positiveDir, malformedDir, missingDir].every(Boolean)) {
  process.stderr.write('usage: gemini-source-loader.mjs <bundle-dir> <positive-dir> <malformed-dir> <missing-dir>\n');
  process.exit(2);
}

let loaderModule;
for (const name of (await fs.readdir(bundleDir)).filter((n) => n.endsWith('.js')).sort()) {
  const file = path.join(bundleDir, name);
  const source = await fs.readFile(file, 'utf8');
  if (!source.includes('async function loadAgentsFromDirectory')) continue;
  const candidate = await import(pathToFileURL(file).href);
  if (typeof candidate.loadAgentsFromDirectory === 'function') {
    loaderModule = candidate;
    break;
  }
}
if (!loaderModule) throw new Error('Gemini CLI bundle did not expose its agent directory loader');

const summarize = async (dir) => {
  const result = await loaderModule.loadAgentsFromDirectory(dir);
  return {
    agentCount: result.agents.length,
    agents: result.agents.map((agent) => ({
      name: agent.name,
      description: agent.description,
      model: agent.modelConfig?.model,
      sourceHash: agent.metadata?.hash,
    })),
    errorCount: result.errors.length,
  };
};

const output = {
  method: 'bundled-source-loader',
  positive: await summarize(positiveDir),
  malformed: await summarize(malformedDir),
  missing: await summarize(missingDir),
};
process.stdout.write(`${JSON.stringify(output, null, 2)}\n`);
