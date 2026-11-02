#!/usr/bin/env node

const readline = require('node:readline/promises');
const { stdin, stdout } = require('node:process');
const { addPath, doctor, install, removePath, rollback, update } = require('./install.js');

function usage() {
  return `Leadfox installer\n\nUsage:\n  leadfox install [options]\n  leadfox update [--check|--rollback] [options]\n  leadfox doctor [options]\n  leadfox path add|remove [options]\n\nOptions:\n  --version VERSION          Select an exact release\n  --root DIR                 Use a test or alternate installation root\n  --release-base-url URL     Override the release source (local fixtures only)\n  --file PATH                Select a shell configuration file for PATH changes\n  --yes                      Confirm a mutating operation\n\nThe installer uses a user-owned directory and verifies release checksums before activation.\n`;
}

function parseArgs(argv) {
  const options = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--help' || arg === '-h') {
      options.help = true;
    } else if (arg === '--version') {
      if (!argv[i + 1]) throw new Error('--version requires a value');
      options.version = argv[++i];
    } else if (arg === '--root') {
      if (!argv[i + 1]) throw new Error('--root requires a directory');
      options.root = argv[++i];
    } else if (arg === '--release-base-url') {
      if (!argv[i + 1]) throw new Error('--release-base-url requires a URL');
      options.releaseBaseUrl = argv[++i];
    } else if (arg === '--yes') {
      options.yes = true;
    } else if (arg === '--check') {
      options.check = true;
    } else if (arg === '--rollback') {
      options.rollback = true;
    } else if (arg === '--force') {
      options.force = true;
    } else if (arg === '--file') {
      if (!argv[i + 1]) throw new Error('--file requires a path');
      options.file = argv[++i];
    } else {
      throw new Error(`unknown option ${arg}`);
    }
  }
  return options;
}

async function main() {
  const command = process.argv[2];
  if (!command || command === 'help' || command === '--help' || command === '-h') {
    process.stdout.write(usage());
    return;
  }
  const pathCommand = command === 'path' ? process.argv[3] : null;
  const options = parseArgs(command === 'path' ? process.argv.slice(4) : process.argv.slice(3));
  if (options.help) {
    process.stdout.write(usage());
    return;
  }
  let result;
  if (command === 'install') {
    result = await install(options);
  } else if (command === 'update') {
    if (options.rollback) result = await rollback(options);
    else if (!options.check && !options.yes) {
      const answer = await confirm('Update Leadfox after verifying the proposed release? [y/N] ');
      if (!answer) throw new Error('update cancelled');
      result = await update(options);
    } else {
      result = await update(options);
    }
  } else if (command === 'doctor') {
    result = await doctor(options);
    if (!result.healthy) process.exitCode = 1;
  } else if (command === 'path') {
    if (pathCommand !== 'add' && pathCommand !== 'remove') throw new Error("use 'leadfox path add' or 'leadfox path remove'");
    if (!options.yes) {
      const answer = await confirm(`${pathCommand === 'add' ? 'Add' : 'Remove'} the Leadfox PATH entry? [y/N] `);
      if (!answer) throw new Error('PATH change cancelled');
    }
    result = pathCommand === 'add' ? await addPath(options) : await removePath(options);
  } else {
    throw new Error(`unsupported command ${command}; use 'leadfox install', 'leadfox update', or 'leadfox doctor'`);
  }
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

async function confirm(question) {
  if (!stdin.isTTY || !stdout.isTTY) throw new Error('confirmation is required; use --yes in non-interactive mode');
  const prompt = readline.createInterface({ input: stdin, output: stdout });
  try {
    const answer = await prompt.question(question);
    return /^y(es)?$/i.test(answer.trim());
  } finally {
    prompt.close();
  }
}

main().catch((error) => {
  process.stderr.write(`Error: ${error.message}\n`);
  process.exitCode = 1;
});
