type MermaidModule = {
    initialize: (config: Record<string, unknown>) => void;
    render: (id: string, text: string) => Promise<{
        svg: string;
        bindFunctions?: (element: Element) => void;
    }>;
};

const mermaidFencePattern = /```mermaid[\t ]*\r?\n[\s\S]*?\r?\n```/i;
const mermaidCodeSelector = [
    'pre code.language-mermaid',
    'pre code.lang-mermaid',
    'pre code[class*="language-mermaid"]',
    'pre code[class*="lang-mermaid"]',
    '.post-code code.language-mermaid',
    '.post-code code.lang-mermaid',
    '.post-code code[class*="language-mermaid"]',
    '.post-code code[class*="lang-mermaid"]',
].join(', ');

const renderedDiagramClassName = 'langflow-mermaid-rendered';
const hiddenSourceAttribute = 'data-langflow-mermaid-hidden';

let mermaidLoader: Promise<MermaidModule> | null = null;
let mermaidInitialized = false;

export function containsCompleteMermaidFence(message: string) {
    return mermaidFencePattern.test(message);
}

export function findMermaidCodeBlocks(container: ParentNode) {
    return Array.from(container.querySelectorAll(mermaidCodeSelector)) as HTMLElement[];
}

export async function renderMermaidDiagrams(container: HTMLElement, postID: string, message: string) {
    cleanupMermaidArtifacts(container);
    if (!containsCompleteMermaidFence(message)) {
        return;
    }

    const mermaidBlocks = findMermaidCodeBlocks(container);
    if (mermaidBlocks.length === 0) {
        return;
    }

    const mermaid = await getMermaid();
    await Promise.all(mermaidBlocks.map(async (codeElement, index) => {
        const definition = extractCodeText(codeElement);
        if (!definition) {
            return;
        }

        const sourceContainer = findSourceContainer(codeElement);
        if (!sourceContainer) {
            return;
        }

        const target = document.createElement('div');
        target.className = renderedDiagramClassName;
        target.setAttribute('data-langflow-mermaid-id', `${postID}-${index}`);

        try {
            const diagramID = buildDiagramID(postID, index);
            const {svg, bindFunctions} = await mermaid.render(diagramID, definition);
            target.innerHTML = svg;
            bindFunctions?.(target);
            sourceContainer.style.display = 'none';
            sourceContainer.setAttribute(hiddenSourceAttribute, 'true');
            sourceContainer.insertAdjacentElement('afterend', target);
        } catch (error) {
            target.remove();
            sourceContainer.style.display = '';
            sourceContainer.removeAttribute(hiddenSourceAttribute);
            sourceContainer.setAttribute('data-langflow-mermaid-error', stringifyError(error));
        }
    }));
}

export function cleanupMermaidArtifacts(container: HTMLElement) {
    for (const rendered of Array.from(container.querySelectorAll(`.${renderedDiagramClassName}`))) {
        rendered.remove();
    }

    for (const hiddenSource of Array.from(container.querySelectorAll(`[${hiddenSourceAttribute}="true"]`)) as HTMLElement[]) {
        hiddenSource.style.display = '';
        hiddenSource.removeAttribute(hiddenSourceAttribute);
        hiddenSource.removeAttribute('data-langflow-mermaid-error');
    }
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

function extractCodeText(codeElement: HTMLElement) {
    return (codeElement.textContent || '').trim();
}

function findSourceContainer(codeElement: HTMLElement) {
    return (codeElement.closest('pre, .post-code') || codeElement) as HTMLElement | null;
}

function buildDiagramID(postID: string, index: number) {
    const normalized = postID.replace(/[^a-zA-Z0-9_-]/g, '');
    return `langflow-mermaid-${normalized}-${index}-${Date.now()}`;
}

function stringifyError(error: unknown) {
    if (error instanceof Error) {
        return error.message;
    }

    return String(error);
}
