const fs = require('fs');
const path = require('path');
const assert = require('assert');

const script = fs.readFileSync(path.join(__dirname, 'shell-integration.ps1'), 'utf8');

assert.match(script, /function Prompt\s*\{/);
assert.match(script, /\$path\s*=\s*\$PWD\.ProviderPath/);
assert.match(script, /PS \$path\$\('>' \* \(\$nestedPromptLevel \+ 1\)\) /);
assert.doesNotMatch(
  script,
  /function Prompt\s*\{[\s\S]*?__dm_CommandEnd\s+__dm_OutputStart\s+__dm_Cwd\s+__dm_PromptStart\s+"PS/,
  'Prompt should return one composed string instead of emitting multiple prompt objects'
);
