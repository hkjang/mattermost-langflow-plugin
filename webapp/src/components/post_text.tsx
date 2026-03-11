import React, {useEffect, useRef} from 'react';
import {useSelector} from 'react-redux';

import type {Channel} from '@mattermost/types/channels';
import type {GlobalState} from '@mattermost/types/store';
import type {Team} from '@mattermost/types/teams';

import {cleanupMermaidArtifacts, containsCompleteMermaidFence, renderMermaidDiagrams} from '../mermaid_rendering';

const cursorClassName = 'langflow-streaming-post-cursor';
const markdownBodyClassName = 'langflow-markdown-body';
const mattermostPostTextClassName = 'post-message__text';

const containerStyle: React.CSSProperties = {
    display: 'block',
    maxWidth: '100%',
    overflow: 'hidden',
    wordBreak: 'break-word',
};

let streamingStylesInjected = false;

type Props = {
    message: string;
    channelID: string;
    postID: string;
    showCursor?: boolean;
};

export default function PostText({message, channelID, postID, showCursor}: Props) {
    const channel = useSelector<GlobalState, Channel | undefined>((state) => state.entities.channels.channels[channelID]);
    const team = useSelector<GlobalState, Team | undefined>((state) => state.entities.teams.teams[channel?.team_id || '']);
    const siteURL = useSelector<GlobalState, string | undefined>((state) => state.entities.general.config.SiteURL);
    const containerRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        ensureStreamingStyles();
    }, []);

    useEffect(() => {
        const cleanup = () => undefined;
        const container = containerRef.current;
        if (!container) {
            return cleanup;
        }

        cleanupMermaidArtifacts(container);
        if (!containsCompleteMermaidFence(message)) {
            return () => {
                cleanupMermaidArtifacts(container);
            };
        }

        let cancelled = false;
        renderMermaidDiagrams(container, postID, message).catch(() => {
            if (!cancelled) {
                cleanupMermaidArtifacts(container);
            }
        });

        return () => {
            cancelled = true;
            cleanupMermaidArtifacts(container);
        };
    }, [message, postID]);

    const postUtils = (window as any).PostUtils as {
        formatText: (value: string, options: Record<string, unknown>) => string;
        messageHtmlToComponent: (value: string, options: Record<string, unknown>) => React.ReactNode;
    } | undefined;

    if (!postUtils) {
        return (
            <div
                className={buildContainerClassName(showCursor)}
                data-testid='posttext'
                ref={containerRef}
                style={containerStyle}
            >
                {message}
                {showCursor && <CursorFallback/>}
            </div>
        );
    }

    const formattedMessage = postUtils.formatText(message, {
        singleline: false,
        mentionHighlight: true,
        atMentions: true,
        team,
        unsafeLinks: false,
        minimumHashtagLength: 1000000000,
        siteURL,
        markdown: true,
    });

    const content = postUtils.messageHtmlToComponent(formattedMessage, {
        hasPluginTooltips: true,
        latex: false,
        inlinelatex: false,
        postId: postID,
    });

    return (
        <div
            className={buildContainerClassName(showCursor)}
            data-testid='posttext'
            ref={containerRef}
            style={containerStyle}
        >
            {content || <p/>}
            {!content && showCursor && <CursorFallback/>}
        </div>
    );
}

function CursorFallback() {
    return (
        <span
            style={{
                animation: 'langflow-stream-cursor-blink 500ms ease-in-out infinite',
                background: 'rgba(var(--center-channel-color-rgb), 0.48)',
                display: 'inline-block',
                height: '16px',
                marginLeft: '3px',
                verticalAlign: 'text-bottom',
                width: '7px',
            }}
        />
    );
}

function ensureStreamingStyles() {
    if (streamingStylesInjected || typeof document === 'undefined') {
        return;
    }

    const style = document.createElement('style');
    style.setAttribute('data-langflow-streaming-cursor', 'true');
    style.textContent = `
@keyframes langflow-stream-cursor-blink {
    0% { opacity: 0.48; }
    20% { opacity: 0.48; }
    100% { opacity: 0.12; }
}

.${cursorClassName} > ul:last-child > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > ol:last-child > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > ul:last-child > li:last-child > span > ul > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > ol:last-child > li:last-child > span > ul > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > ul:last-child > li:last-child > span > ol > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > ol:last-child > li:last-child > span > ol > li:last-child > span:not(:has(li))::after,
.${cursorClassName} > h1:last-child::after,
.${cursorClassName} > h2:last-child::after,
.${cursorClassName} > h3:last-child::after,
.${cursorClassName} > h4:last-child::after,
.${cursorClassName} > h5:last-child::after,
.${cursorClassName} > h6:last-child::after,
.${cursorClassName} > blockquote:last-child > p::after,
.${cursorClassName} > p:last-child::after,
.${cursorClassName} > p:empty::after {
    content: '';
    width: 7px;
    height: 16px;
    background: rgba(var(--center-channel-color-rgb), 0.48);
    display: inline-block;
    margin-left: 3px;
    animation: langflow-stream-cursor-blink 500ms ease-in-out infinite;
}

.${markdownBodyClassName} table,
.${markdownBodyClassName} .markdown__table {
    border-collapse: collapse;
    border-spacing: 0;
    display: block;
    margin: 12px 0;
    max-width: 100%;
    min-width: 100%;
    overflow-x: auto;
    width: max-content;
}

.${markdownBodyClassName} thead {
    background: rgba(var(--center-channel-color-rgb), 0.04);
}

.${markdownBodyClassName} th,
.${markdownBodyClassName} td {
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
    padding: 8px 12px;
    vertical-align: top;
    white-space: pre-wrap;
    word-break: break-word;
}

.${markdownBodyClassName} pre,
.${markdownBodyClassName} .post-code {
    max-width: 100%;
    overflow-x: auto;
}

.${markdownBodyClassName} img {
    height: auto;
    max-width: 100%;
}

.${markdownBodyClassName} .langflow-mermaid-rendered {
    background: rgba(var(--center-channel-color-rgb), 0.03);
    border: 1px solid rgba(var(--center-channel-color-rgb), 0.12);
    border-radius: 12px;
    margin: 12px 0;
    max-width: 100%;
    overflow-x: auto;
    padding: 12px;
}

.${markdownBodyClassName} .langflow-mermaid-rendered svg {
    display: block;
    height: auto;
    margin: 0 auto;
    max-width: 100%;
}
`;
    document.head.appendChild(style);
    streamingStylesInjected = true;
}

function buildContainerClassName(showCursor?: boolean) {
    return [
        mattermostPostTextClassName,
        markdownBodyClassName,
        showCursor ? cursorClassName : '',
    ].filter(Boolean).join(' ');
}
