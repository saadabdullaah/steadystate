import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
test('golden page preserves runtime identity and API boundary',async()=>{const [page,source]=await Promise.all([readFile(new URL('../index.html',import.meta.url),'utf8'),readFile(new URL('../src/main.tsx',import.meta.url),'utf8')]);assert.match(page,/src\/main\.tsx/);assert.match(source,/STEADYSTATE SERVICE/);});
