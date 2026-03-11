/**
 * @jest-environment jsdom
 */

import {cleanupMermaidArtifacts, containsCompleteMermaidFence, findMermaidCodeBlocks} from './mermaid_rendering';

test('containsCompleteMermaidFence matches only closed mermaid fences', () => {
    expect(containsCompleteMermaidFence('```mermaid\ngraph TD\nA-->B\n```')).toBe(true);
    expect(containsCompleteMermaidFence('```mermaid\ngraph TD\nA-->B')).toBe(false);
});

test('findMermaidCodeBlocks locates mermaid code elements', () => {
    document.body.innerHTML = `
        <div>
            <pre><code class="language-mermaid">graph TD\nA-->B</code></pre>
            <pre><code class="language-js">console.log('nope')</code></pre>
        </div>
    `;

    const nodes = findMermaidCodeBlocks(document.body);
    expect(nodes).toHaveLength(1);
    expect(nodes[0].textContent).toContain('graph TD');
});

test('cleanupMermaidArtifacts removes rendered diagrams and restores hidden sources', () => {
    document.body.innerHTML = `
        <div id="root">
            <pre data-langflow-mermaid-hidden="true" style="display:none"><code class="language-mermaid">graph TD\nA-->B</code></pre>
            <div class="langflow-mermaid-rendered"><svg></svg></div>
        </div>
    `;

    const root = document.getElementById('root') as HTMLElement;
    cleanupMermaidArtifacts(root);

    expect(root.querySelector('.langflow-mermaid-rendered')).toBeNull();
    const source = root.querySelector('pre') as HTMLElement;
    expect(source.style.display).toBe('');
    expect(source.hasAttribute('data-langflow-mermaid-hidden')).toBe(false);
});
