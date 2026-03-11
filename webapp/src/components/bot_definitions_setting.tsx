import React, {useEffect, useMemo, useState} from 'react';

type DraftBotDefinition = {
    id?: string;
    username?: string;
    display_name?: string;
    description?: string;
    flow_id?: string;
    include_context_by_default?: boolean;
    allowed_teams?: string[];
    allowed_channels?: string[];
    allowed_users?: string[];
    input_schema?: Array<{
        name?: string;
        label?: string;
        description?: string;
        type?: string;
        required?: boolean;
        placeholder?: string;
        default_value?: unknown;
    }>;
};

type CustomSettingProps = {
    id?: string;
    value?: string;
    disabled?: boolean;
    setByEnv?: boolean;
    helpText?: React.ReactNode;
    informChange: (name: string, value: string) => void;
};

const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '16px',
};

const cardStyle: React.CSSProperties = {
    background: 'rgba(var(--center-channel-color-rgb), 0.04)',
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.12)',
    borderRadius: '12px',
    display: 'flex',
    flexDirection: 'column',
    gap: '10px',
    padding: '16px',
};

const textareaStyle: React.CSSProperties = {
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.16)',
    borderRadius: '8px',
    fontFamily: 'monospace',
    minHeight: '280px',
    padding: '12px',
    resize: 'vertical',
    width: '100%',
};

const sampleBotCatalog = JSON.stringify([
    {
        id: 'thread-summary-bot',
        username: 'thread-summary-bot',
        display_name: 'Thread Summary Bot',
        description: 'Summarizes the current thread with action items.',
        flow_id: 'thread-summary',
        include_context_by_default: true,
        allowed_teams: ['engineering'],
        allowed_channels: ['town-square'],
        allowed_users: [],
        input_schema: [
            {
                name: 'tone',
                label: 'Tone',
                type: 'text',
                placeholder: 'concise',
                default_value: 'concise',
            },
        ],
    },
    {
        id: 'support-assistant-bot',
        username: 'support-assistant-bot',
        display_name: 'Support Assistant',
        description: 'Answers customer support questions with the mapped Langflow flow.',
        flow_id: 'support-assistant',
        include_context_by_default: true,
        input_schema: [],
    },
], null, 2);

export default function BotDefinitionsSetting(props: CustomSettingProps) {
    const settingKey = props.id || 'BotDefinitions';
    const initialValue = props.value || '';
    const [rawValue, setRawValue] = useState(initialValue);

    useEffect(() => {
        setRawValue(initialValue);
    }, [initialValue]);

    const parsed = useMemo(() => {
        if (!rawValue.trim()) {
            return {items: [] as DraftBotDefinition[], error: ''};
        }

        try {
            const value = JSON.parse(rawValue) as DraftBotDefinition[];
            if (!Array.isArray(value)) {
                return {items: [] as DraftBotDefinition[], error: 'Bot definitions must be a JSON array.'};
            }
            return {items: value, error: ''};
        } catch (error) {
            return {items: [] as DraftBotDefinition[], error: (error as Error).message};
        }
    }, [rawValue]);

    const hasSampleLoaded = rawValue.trim() === sampleBotCatalog.trim();

    return (
        <div style={containerStyle}>
            <section style={cardStyle}>
                <strong>{'Bot Catalog'}</strong>
                <span style={{fontSize: '12px', opacity: 0.8}}>
                    {'Create one Mattermost bot per Langflow flow. Each bot listens in DM or on @mention, then calls POST /api/v1/run/$FLOW_ID with a JSON body containing only input_value.'}
                </span>
                <span style={{fontSize: '12px', opacity: 0.8}}>
                    {'Recommended fields per bot: id, username, display_name, flow_id, description, include_context_by_default, allowed_teams, allowed_channels, allowed_users, input_schema.'}
                </span>
                <textarea
                    disabled={props.disabled || props.setByEnv}
                    onChange={(event) => {
                        const nextValue = event.target.value;
                        setRawValue(nextValue);
                        props.informChange(settingKey, nextValue);
                    }}
                    style={textareaStyle}
                    value={rawValue}
                />
                <div style={{display: 'flex', gap: '8px', flexWrap: 'wrap'}}>
                    <button
                        className='btn btn-secondary'
                        disabled={props.disabled || props.setByEnv || hasSampleLoaded}
                        onClick={() => {
                            setRawValue(sampleBotCatalog);
                            props.informChange(settingKey, sampleBotCatalog);
                        }}
                        type='button'
                    >
                        {'Load sample catalog'}
                    </button>
                    <button
                        className='btn btn-secondary'
                        disabled={props.disabled || props.setByEnv || !rawValue}
                        onClick={() => {
                            setRawValue('');
                            props.informChange(settingKey, '');
                        }}
                        type='button'
                    >
                        {'Clear'}
                    </button>
                </div>
                {props.setByEnv && (
                    <span style={{color: 'var(--error-text)', fontSize: '12px'}}>
                        {'This setting is managed by environment configuration and cannot be edited here.'}
                    </span>
                )}
                {parsed.error && (
                    <span style={{color: 'var(--error-text)', fontSize: '12px'}}>
                        {`JSON error: ${parsed.error}`}
                    </span>
                )}
            </section>

            <section style={cardStyle}>
                <strong>{'Catalog Preview'}</strong>
                {parsed.items.length === 0 && !parsed.error && (
                    <span style={{fontSize: '12px', opacity: 0.8}}>
                        {'No bots configured yet. Add a bot definition to create its Mattermost bot account and bind it to a Langflow flow.'}
                    </span>
                )}
                {parsed.items.map((bot, index) => (
                    <div
                        key={`${bot.id || bot.username || 'bot'}-${index}`}
                        style={{
                            background: 'rgba(var(--center-channel-color-rgb), 0.03)',
                            border: '1px solid rgba(var(--center-channel-color-rgb), 0.1)',
                            borderRadius: '10px',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: '6px',
                            padding: '12px',
                        }}
                    >
                        <strong>{bot.display_name || bot.username || bot.id || `Bot ${index + 1}`}</strong>
                        <span>{`Mention: @${bot.username || 'username'}`}</span>
                        <span>{`Flow: ${bot.flow_id || 'flow-id'}`}</span>
                        {bot.description && <span>{bot.description}</span>}
                        <code style={{whiteSpace: 'pre-wrap'}}>
                            {`curl -X POST "$LANGFLOW_BASE_URL/api/v1/run/${bot.flow_id || '$FLOW_ID'}" \\\n  -H "Authorization: Bearer $LANGFLOW_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "input_value": "Hello from @${bot.username || 'bot'}"\n  }'`}
                        </code>
                    </div>
                ))}
            </section>
        </div>
    );
}
