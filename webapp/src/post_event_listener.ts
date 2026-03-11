import type {WebSocketMessage} from '@mattermost/client';

import type {StreamingPostUpdateEventData} from './streaming';

type PostUpdateListener = (msg: WebSocketMessage<StreamingPostUpdateEventData>) => void;

export default class PostEventListener {
    private readonly listeners = new Map<string, Set<PostUpdateListener>>();

    public registerPostUpdateListener = (postID: string, listener: PostUpdateListener) => {
        const normalizedPostID = normalizePostID(postID);
        if (!normalizedPostID) {
            return;
        }

        const postListeners = this.listeners.get(normalizedPostID) || new Set<PostUpdateListener>();
        postListeners.add(listener);
        this.listeners.set(normalizedPostID, postListeners);
    };

    public unregisterPostUpdateListener = (postID: string, listener: PostUpdateListener) => {
        const normalizedPostID = normalizePostID(postID);
        if (!normalizedPostID) {
            return;
        }

        const postListeners = this.listeners.get(normalizedPostID);
        if (!postListeners) {
            return;
        }

        postListeners.delete(listener);
        if (postListeners.size === 0) {
            this.listeners.delete(normalizedPostID);
        }
    };

    public handlePostUpdateWebsockets = (msg: WebSocketMessage<StreamingPostUpdateEventData>) => {
        const postID = normalizePostID(msg?.data?.post_id);
        if (!postID) {
            return;
        }

        const listeners = this.listeners.get(postID);
        if (!listeners || listeners.size === 0) {
            return;
        }

        for (const listener of listeners) {
            listener(msg);
        }
    };
}

function normalizePostID(value?: string) {
    if (typeof value !== 'string') {
        return '';
    }

    return value.trim();
}
