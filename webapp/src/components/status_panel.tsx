import React, {useEffect, useState} from 'react';

import type {ConnectionStatus, PluginStatus} from '../client';
import {getStatus, testConnection} from '../client';

const cardStyle: React.CSSProperties = {
    background: 'rgba(var(--center-channel-color-rgb), 0.04)',
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.12)',
    borderRadius: '12px',
    display: 'flex',
    flexDirection: 'column',
    gap: '12px',
    padding: '16px',
};

export default function StatusPanel() {
    const [status, setStatus] = useState<PluginStatus | null>(null);
    const [connection, setConnection] = useState<ConnectionStatus | null>(null);
    const [message, setMessage] = useState('');
    const [loading, setLoading] = useState(true);
    const [testing, setTesting] = useState(false);

    useEffect(() => {
        let cancelled = false;
        async function load() {
            try {
                const pluginStatus = await getStatus();
                if (!cancelled) {
                    setStatus(pluginStatus);
                }
            } catch (error) {
                if (!cancelled) {
                    setMessage((error as Error).message);
                }
            } finally {
                if (!cancelled) {
                    setLoading(false);
                }
            }
        }
        load();
        return () => {
            cancelled = true;
        };
    }, []);

    async function onTestConnection() {
        setTesting(true);
        setMessage('');
        try {
            setConnection(await testConnection());
        } catch (error) {
            setMessage((error as Error).message);
        } finally {
            setTesting(false);
        }
    }

    return (
        <div style={cardStyle}>
            <strong>{'Langflow Status'}</strong>
            {loading && <span>{'Loading plugin status...'}</span>}
            {!loading && status && (
                <>
                    <div>{`Base URL: ${status.base_url || 'Not configured'}`}</div>
                    <div>{`Configured bots: ${status.bot_count}`}</div>
                    <div>{`Allowed hosts: ${(status.allow_hosts || []).join(', ') || 'Uses Langflow host'}`}</div>
                    <div>{`Streaming replies: ${status.streaming_enabled ? 'enabled' : 'disabled'}`}</div>
                    <div>{`Streaming update interval: ${status.streaming_update_interval_ms || 0} ms`}</div>
                    {status.config_error && <div>{`Config error: ${status.config_error}`}</div>}
                    {status.bot_sync?.last_error && <div>{`Bot sync error: ${status.bot_sync.last_error}`}</div>}
                    {(status.bots || []).length > 0 && (
                        <div style={{display: 'flex', flexDirection: 'column', gap: '10px'}}>
                            {(status.bots || []).map((bot) => {
                                const managed = (status.managed_bots || []).find((item) => item.bot_id === bot.id);
                                return (
                                    <div
                                        key={bot.id}
                                        style={{
                                            background: 'rgba(var(--center-channel-color-rgb), 0.03)',
                                            border: '1px solid rgba(var(--center-channel-color-rgb), 0.1)',
                                            borderRadius: '10px',
                                            display: 'flex',
                                            flexDirection: 'column',
                                            gap: '4px',
                                            padding: '12px',
                                        }}
                                    >
                                        <strong>{bot.display_name || bot.username}</strong>
                                        <span>{`@${bot.username} -> ${bot.flow_id}`}</span>
                                        {managed && <span>{`Mattermost user: ${managed.user_id || 'Pending creation'}`}</span>}
                                        {managed && <span>{`Managed: ${managed.registered ? 'yes' : 'no'}, active: ${managed.active ? 'yes' : 'no'}`}</span>}
                                        {bot.description && <span>{bot.description}</span>}
                                    </div>
                                );
                            })}
                        </div>
                    )}
                    <button
                        className='btn btn-primary'
                        disabled={testing}
                        onClick={onTestConnection}
                        type='button'
                    >
                        {testing ? 'Testing...' : 'Test connection'}
                    </button>
                    {connection && (
                        <div>
                            <div>{connection.ok ? 'Connection succeeded' : 'Connection failed'}</div>
                            <div>{connection.url}</div>
                            <div>{connection.message}</div>
                        </div>
                    )}
                </>
            )}
            {message && <span>{message}</span>}
            <div style={{fontSize: '12px', opacity: 0.8}}>
                {'Saving System Console settings creates or updates the Mattermost bot accounts owned by this plugin. Removing a bot from the catalog deactivates that plugin-managed bot.'}
            </div>
            <div style={{fontSize: '12px', opacity: 0.8}}>
                {'Users can then DM the bot or @mention it in Mattermost, and the plugin will call the mapped Langflow run API for that bot.'}
            </div>
            <div style={{fontSize: '12px', opacity: 0.8}}>
                {'When streaming is enabled, the plugin posts one bot reply first and then updates that same post as Langflow tokens arrive.'}
            </div>
        </div>
    );
}
