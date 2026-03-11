import React, {useEffect, useMemo, useState} from 'react';

import type {WebSocketMessage} from '@mattermost/client';

import PostText from './post_text';

type PostUpdateData = {
    post_id?: string;
    next?: string;
    control?: string;
};

type Props = {
    post: any;
    websocketRegister: (postID: string, listener: (msg: WebSocketMessage<PostUpdateData>) => void) => void;
    websocketUnregister: (postID: string, listener: (msg: WebSocketMessage<PostUpdateData>) => void) => void;
};

const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '8px',
};

const statusStyle: React.CSSProperties = {
    color: 'rgba(var(--center-channel-color-rgb), 0.72)',
    fontSize: '12px',
    fontWeight: 600,
    letterSpacing: '0.01em',
};

export default function LangflowBotPost(props: Props) {
    const [message, setMessage] = useState(props.post.message || '');
    const [generating, setGenerating] = useState(isStreamingPost(props.post));

    useEffect(() => {
        setMessage(props.post.message || '');
        setGenerating(isStreamingPost(props.post));
    }, [props.post.message, props.post.props?.langflow_streaming, props.post.props?.langflow_stream_status]);

    const listener = useMemo(() => {
        return (msg: WebSocketMessage<PostUpdateData>) => {
            const data = msg?.data || {};
            if (data.control === 'start') {
                setGenerating(true);
                return;
            }

            if (typeof data.next === 'string' && data.next !== '') {
                setGenerating(true);
                setMessage(data.next);
            }

            if (data.control === 'end') {
                setGenerating(false);
            }
        };
    }, []);

    useEffect(() => {
        props.websocketRegister(props.post.id, listener);
        return () => {
            props.websocketUnregister(props.post.id, listener);
        };
    }, [listener, props.post.id, props.websocketRegister, props.websocketUnregister]);

    return (
        <div
            data-testid='langflow-bot-post'
            style={containerStyle}
        >
            <PostText
                channelID={props.post.channel_id}
                message={message}
                postID={props.post.id}
                showCursor={generating}
            />
            {generating && (
                <span style={statusStyle}>
                    {'응답 생성 중...'}
                </span>
            )}
        </div>
    );
}

function isStreamingPost(post: any) {
    return post?.props?.langflow_streaming === 'true' || post?.props?.langflow_stream_status === 'streaming';
}
