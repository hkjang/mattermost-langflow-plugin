import {containsCompleteMermaidFence, splitRenderableMessage} from './mermaid_rendering';

test('containsCompleteMermaidFence matches only closed mermaid fences', () => {
    expect(containsCompleteMermaidFence('```mermaid\ngraph TD\nA-->B\n```')).toBe(true);
    expect(containsCompleteMermaidFence('```mermaid\ngraph TD\nA-->B')).toBe(false);
});

test('splitRenderableMessage separates text and mermaid segments', () => {
    const segments = splitRenderableMessage([
        '서두 문장',
        '```mermaid',
        'graph TD',
        'A-->B',
        '```',
        '마무리 문장',
    ].join('\n'));

    expect(segments).toEqual([
        {kind: 'text', content: '서두 문장\n'},
        {kind: 'mermaid', content: 'graph TD\nA-->B'},
        {kind: 'text', content: '\n마무리 문장'},
    ]);
});

test('splitRenderableMessage keeps plain text when mermaid fence is incomplete', () => {
    const message = '```mermaid\ngraph TD\nA-->B';
    expect(splitRenderableMessage(message)).toEqual([{kind: 'text', content: message}]);
});
