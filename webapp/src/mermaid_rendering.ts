type MermaidModule = {
    initialize: (config: Record<string, unknown>) => void;
    render: (id: string, text: string) => Promise<{
        svg: string;
        bindFunctions?: (element: Element) => void;
    }>;
};

export type RenderableMessageSegment = {
    kind: 'text' | 'mermaid';
    content: string;
};

const mermaidFencePattern = /```mermaid[\t ]*\n([\s\S]*?)\n```/gi;
let mermaidInitialized = false;
let mermaidModule: MermaidModule | null = null;

export function containsCompleteMermaidFence(message: string) {
    return (/```mermaid[\t ]*\n[\s\S]*?\n```/i).test(normalizeRenderableMessage(message));
}

export function normalizeRenderableMessage(message: string) {
    const normalizedLines = normalizeMarkdownTableLines(
        normalizeMermaidLines((message || '').replace(/\r\n/g, '\n')).split('\n'),
    );
    return normalizedLines.join('\n').replace(/\n{3,}/g, '\n\n');
}

export function splitRenderableMessage(message: string): RenderableMessageSegment[] {
    const segments: RenderableMessageSegment[] = [];
    const text = normalizeRenderableMessage(message);
    const matcher = new RegExp(mermaidFencePattern);

    let lastIndex = 0;
    let match = matcher.exec(text);
    while (match) {
        const fullMatch = match[0];
        const definition = (match[1] || '').trim();
        const matchIndex = match.index;

        if (matchIndex > lastIndex) {
            segments.push({
                kind: 'text',
                content: text.slice(lastIndex, matchIndex),
            });
        }

        if (definition) {
            segments.push({
                kind: 'mermaid',
                content: definition,
            });
        } else {
            segments.push({
                kind: 'text',
                content: fullMatch,
            });
        }

        lastIndex = matchIndex + fullMatch.length;
        match = matcher.exec(text);
    }

    if (lastIndex < text.length) {
        segments.push({
            kind: 'text',
            content: text.slice(lastIndex),
        });
    }

    if (segments.length === 0) {
        return [{kind: 'text', content: text}];
    }

    return segments.filter((segment, index) => (
        segment.kind === 'mermaid' ||
        segment.content !== '' ||
        index === segments.length - 1
    ));
}

export async function renderMermaidDefinition(definition: string, postID: string, index: number) {
    const mermaid = getMermaidModule();
    if (!mermaidInitialized) {
        mermaid.initialize({
            startOnLoad: false,
            securityLevel: 'strict',
            theme: 'neutral',
            fontFamily: 'inherit',
        });
        mermaidInitialized = true;
    }
    return mermaid.render(buildDiagramID(postID, index), definition);
}

function getMermaidModule() {
    if (!mermaidModule) {
        // eslint-disable-next-line global-require
        mermaidModule = require('mermaid/dist/mermaid.js') as MermaidModule;
    }

    return mermaidModule;
}

function buildDiagramID(postID: string, index: number) {
    const normalized = postID.replace(/[^a-zA-Z0-9_-]/g, '');
    return `langflow-mermaid-${normalized}-${index}-${Date.now()}`;
}

function normalizeMermaidLines(linesText: string) {
    const lines = linesText.split('\n');
    const normalized: string[] = [];
    let inMermaidBlock = false;
    let mermaidIndent = '';

    for (const line of lines) {
        if (!inMermaidBlock) {
            const openingMatch = line.match(/^(\s*)```mermaid\b(.*)$/i);
            if (openingMatch) {
                inMermaidBlock = true;
                mermaidIndent = openingMatch[1] || '';
                normalized.push(`\`\`\`mermaid${openingMatch[2] || ''}`.trimEnd());
                continue;
            }
            normalized.push(line);
            continue;
        }

        if ((/^\s*```/).test(line)) {
            inMermaidBlock = false;
            mermaidIndent = '';
            normalized.push('```');
            continue;
        }

        if (mermaidIndent && line.startsWith(mermaidIndent)) {
            normalized.push(line.slice(mermaidIndent.length));
            continue;
        }

        normalized.push(line.replace(/^\s{1,4}/, ''));
    }

    return normalized.join('\n');
}

function normalizeMarkdownTableLines(lines: string[]) {
    const compacted = lines.map((line) => line.replace(/^\s+(?=\|)/, ''));
    const withoutInnerBlanks: string[] = [];

    for (let index = 0; index < compacted.length; index++) {
        const line = compacted[index];
        if (line.trim() !== '') {
            withoutInnerBlanks.push(line);
            continue;
        }

        const previousIndex = findPreviousNonEmptyIndex(compacted, index);
        const nextIndex = findNextNonEmptyIndex(compacted, index);
        const betweenTableLines = previousIndex >= 0 &&
            nextIndex >= 0 &&
            isMarkdownTableLine(compacted[previousIndex]) &&
            isMarkdownTableLine(compacted[nextIndex]);

        if (!betweenTableLines) {
            withoutInnerBlanks.push(line);
        }
    }

    const normalized: string[] = [];
    for (let index = 0; index < withoutInnerBlanks.length; index++) {
        const line = withoutInnerBlanks[index];
        const previous = normalized[normalized.length - 1] || '';
        const previousIsTable = isMarkdownTableLine(previous);
        const currentIsTable = isMarkdownTableLine(line);

        if (currentIsTable && previous.trim() !== '' && !previousIsTable) {
            normalized.push('');
        }

        if (!currentIsTable && line.trim() !== '' && previousIsTable) {
            normalized.push('');
        }

        normalized.push(line);
    }

    return normalized;
}

function isMarkdownTableLine(line: string) {
    const trimmed = line.trim();
    if (!trimmed) {
        return false;
    }
    if (!trimmed.startsWith('|') || !trimmed.endsWith('|')) {
        return false;
    }
    return true;
}

function findPreviousNonEmptyIndex(lines: string[], startIndex: number) {
    for (let index = startIndex - 1; index >= 0; index--) {
        if (lines[index].trim() !== '') {
            return index;
        }
    }
    return -1;
}

function findNextNonEmptyIndex(lines: string[], startIndex: number) {
    for (let index = startIndex + 1; index < lines.length; index++) {
        if (lines[index].trim() !== '') {
            return index;
        }
    }
    return -1;
}
