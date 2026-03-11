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

const mermaidFencePattern = /```mermaid[\t ]*\r?\n([\s\S]*?)\r?\n```/gi;

let mermaidLoader: Promise<MermaidModule> | null = null;
let mermaidInitialized = false;

export function containsCompleteMermaidFence(message: string) {
    return (/```mermaid[\t ]*\r?\n[\s\S]*?\r?\n```/i).test(message);
}

export function splitRenderableMessage(message: string): RenderableMessageSegment[] {
    const segments: RenderableMessageSegment[] = [];
    const text = message || '';
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
    const mermaid = await getMermaid();
    return mermaid.render(buildDiagramID(postID, index), definition);
}

async function getMermaid() {
    if (!mermaidLoader) {
        mermaidLoader = import('mermaid').then((module) => {
            const mermaid = (module.default || module) as MermaidModule;
            if (!mermaidInitialized) {
                mermaid.initialize({
                    startOnLoad: false,
                    securityLevel: 'strict',
                    suppressErrorRendering: true,
                    theme: 'neutral',
                    fontFamily: 'inherit',
                });
                mermaidInitialized = true;
            }
            return mermaid;
        });
    }

    return mermaidLoader;
}

function buildDiagramID(postID: string, index: number) {
    const normalized = postID.replace(/[^a-zA-Z0-9_-]/g, '');
    return `langflow-mermaid-${normalized}-${index}-${Date.now()}`;
}
