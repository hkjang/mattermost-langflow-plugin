import React, {useEffect, useRef, useState} from 'react';

import {renderMermaidDefinition} from '../mermaid_rendering';

type Props = {
    definition: string;
    postID: string;
    index: number;
};

export default function MermaidDiagram({definition, postID, index}: Props) {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const [error, setError] = useState('');

    useEffect(() => {
        const container = containerRef.current;
        if (!container) {
            return () => undefined;
        }

        let cancelled = false;
        container.innerHTML = '';
        setError('');

        renderMermaidDefinition(definition, postID, index).then(({svg, bindFunctions}) => {
            if (cancelled || !containerRef.current) {
                return;
            }
            containerRef.current.innerHTML = svg;
            bindFunctions?.(containerRef.current);
        }).catch((renderError: unknown) => {
            if (cancelled) {
                return;
            }
            const message = renderError instanceof Error ? renderError.message : String(renderError);
            setError(message);
        });

        return () => {
            cancelled = true;
            if (containerRef.current) {
                containerRef.current.innerHTML = '';
            }
        };
    }, [definition, index, postID]);

    if (error) {
        return (
            <div className='langflow-mermaid-fallback'>
                <pre className='post-code'>
                    <code className='language-mermaid'>{definition}</code>
                </pre>
                <div style={{fontSize: '12px', opacity: 0.72}}>
                    {`Mermaid 렌더링 실패: ${error}`}
                </div>
            </div>
        );
    }

    return (
        <div className='langflow-mermaid-rendered'>
            <div
                data-testid='langflow-mermaid-diagram'
                ref={containerRef}
            />
        </div>
    );
}
