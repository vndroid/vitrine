import fs from 'node:fs';
import path from 'node:path';
import {globSync} from 'glob';

const includePattern = /^([ \t]*)\/\/[ \t]*@include[ \t]+(["'])(.+?)\2[; \t]*$/gm;

function expand(file, content, stack = []) {
    if (stack.includes(file)) throw new Error(`circular include: ${file}`);
    const nextStack = [...stack, file];
    return content.replace(includePattern, (match, indent, quote, reference) => {
        const pattern = path.normalize(path.resolve(path.dirname(file), reference));
        const matches = globSync(pattern, {dot: true}).map(source => path.resolve(source)).sort();
        if (!matches.length) throw new Error(`include not found: ${reference} (from ${file})`);
        return matches.map(source => {
            let included = fs.readFileSync(source, 'utf8').replace(/;?(\s*)$/, ';$1');
            included = expand(source, included, nextStack);
            return indent + included.replace(/\n/g, `\n${indent}`).replace(/^\s+$/gm, '');
        }).join('\n\n');
    });
}

export default ({file, content}) => expand(file, content);
