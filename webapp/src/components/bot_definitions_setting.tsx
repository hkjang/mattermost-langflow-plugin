import React, {useEffect, useMemo, useState} from 'react';

type InputFieldType = 'text' | 'textarea' | 'number' | 'bool';

type DraftInputField = {
    id: string;
    name: string;
    label: string;
    description: string;
    type: InputFieldType;
    required: boolean;
    placeholder: string;
    default_value: string | number | boolean;
};

type DraftBotDefinition = {
    local_id: string;
    id: string;
    username: string;
    display_name: string;
    description: string;
    flow_id: string;
    include_context_by_default: boolean;
    allowed_teams: string[];
    allowed_channels: string[];
    allowed_users: string[];
    input_schema: DraftInputField[];
};

type StoredInputField = {
    name?: string;
    label?: string;
    description?: string;
    type?: string;
    required?: boolean;
    placeholder?: string;
    default_value?: unknown;
};

type StoredBotDefinition = {
    id?: string;
    username?: string;
    display_name?: string;
    description?: string;
    flow_id?: string;
    include_context_by_default?: boolean;
    allowed_teams?: string[];
    allowed_channels?: string[];
    allowed_users?: string[];
    input_schema?: StoredInputField[];
};

type CustomSettingProps = {
    id?: string;
    value?: unknown;
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

const layoutStyle: React.CSSProperties = {
    display: 'grid',
    gap: '16px',
    gridTemplateColumns: '320px minmax(0, 1fr)',
};

const cardStyle: React.CSSProperties = {
    background: 'rgba(var(--center-channel-color-rgb), 0.04)',
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.12)',
    borderRadius: '12px',
    display: 'flex',
    flexDirection: 'column',
    gap: '12px',
    padding: '16px',
};

const fieldStyle: React.CSSProperties = {
    border: '1px solid rgba(var(--center-channel-color-rgb), 0.16)',
    borderRadius: '8px',
    padding: '10px 12px',
    width: '100%',
};

const textAreaStyle: React.CSSProperties = {
    ...fieldStyle,
    minHeight: '96px',
    resize: 'vertical',
};

const gridTwoStyle: React.CSSProperties = {
    display: 'grid',
    gap: '12px',
    gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
};

const columnStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '8px',
};

const botListItemStyle = (selected: boolean): React.CSSProperties => ({
    background: selected ? 'rgba(var(--button-bg-rgb), 0.12)' : 'rgba(var(--center-channel-color-rgb), 0.03)',
    border: `1px solid ${selected ? 'rgba(var(--button-bg-rgb), 0.36)' : 'rgba(var(--center-channel-color-rgb), 0.10)'}`,
    borderRadius: '10px',
    cursor: 'pointer',
    display: 'flex',
    flexDirection: 'column',
    gap: '4px',
    padding: '12px',
    textAlign: 'left',
    width: '100%',
});

const codeStyle: React.CSSProperties = {
    background: 'rgba(var(--center-channel-color-rgb), 0.06)',
    borderRadius: '8px',
    fontFamily: 'monospace',
    fontSize: '12px',
    padding: '12px',
    whiteSpace: 'pre-wrap',
};

const sampleBots: StoredBotDefinition[] = [
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
        allowed_teams: [],
        allowed_channels: [],
        allowed_users: [],
        input_schema: [],
    },
];

export default function BotDefinitionsSetting(props: CustomSettingProps) {
    const settingKey = props.id || 'BotDefinitions';
    const [bots, setBots] = useState<DraftBotDefinition[]>([]);
    const [selectedBotId, setSelectedBotId] = useState('');
    const [loadError, setLoadError] = useState('');

    useEffect(() => {
        const parsed = parseStoredBots(props.value);
        setBots(parsed.bots);
        setLoadError(parsed.error);
        setSelectedBotId((current) => {
            if (parsed.bots.length === 0) {
                return '';
            }
            if (current && parsed.bots.some((bot) => bot.local_id === current)) {
                return current;
            }
            return parsed.bots[0].local_id;
        });
    }, [props.value]);

    const selectedBot = useMemo(
        () => bots.find((bot) => bot.local_id === selectedBotId) || bots[0] || null,
        [bots, selectedBotId],
    );

    const validationMessages = useMemo(() => validateBots(bots), [bots]);
    const disabled = Boolean(props.disabled || props.setByEnv);

    const syncBots = (nextBots: DraftBotDefinition[], nextSelectedBotId?: string) => {
        setBots(nextBots);
        props.informChange(settingKey, serializeBots(nextBots));

        if (nextBots.length === 0) {
            setSelectedBotId('');
            return;
        }

        if (nextSelectedBotId) {
            setSelectedBotId(nextSelectedBotId);
            return;
        }

        setSelectedBotId((current) => {
            if (current && nextBots.some((bot) => bot.local_id === current)) {
                return current;
            }
            return nextBots[0].local_id;
        });
    };

    const updateBot = (localID: string, updater: (bot: DraftBotDefinition) => DraftBotDefinition) => {
        const nextBots = bots.map((bot) => (
            bot.local_id === localID ? updater(bot) : bot
        ));
        syncBots(nextBots, localID);
    };

    const addBot = () => {
        const bot = createEmptyBot();
        syncBots([...bots, bot], bot.local_id);
    };

    const duplicateBot = () => {
        if (!selectedBot) {
            return;
        }
        const duplicate = cloneBot(selectedBot);
        syncBots([...bots, duplicate], duplicate.local_id);
    };

    const removeSelectedBot = () => {
        if (!selectedBot) {
            return;
        }
        const nextBots = bots.filter((bot) => bot.local_id !== selectedBot.local_id);
        syncBots(nextBots);
    };

    const loadSampleBots = () => {
        const nextBots = sampleBots.map(normalizeStoredBot);
        syncBots(nextBots, nextBots[0]?.local_id);
    };

    return (
        <div style={containerStyle}>
            <section style={cardStyle}>
                <strong>{'Langflow Bot Catalog'}</strong>
                <span style={{fontSize: '12px', opacity: 0.8}}>
                    {'Register multiple Mattermost bots here. Each bot is permanently bound to one Langflow flow and can be invoked by DM or @mention.'}
                </span>
                <span style={{fontSize: '12px', opacity: 0.8}}>
                    {'When a bot runs, the plugin calls POST /api/v1/run/$FLOW_ID and sends a JSON body with input_value built from the prompt, optional form fields, and optional conversation context.'}
                </span>
                <span style={{fontSize: '12px', opacity: 0.8}}>
                    {'After you click Save in the System Console, the plugin creates or updates the matching Mattermost bot accounts automatically.'}
                </span>
                {props.helpText}
                {props.setByEnv && (
                    <span style={{color: 'var(--error-text)', fontSize: '12px'}}>
                        {'This setting is managed by environment configuration and cannot be edited here.'}
                    </span>
                )}
                {loadError && (
                    <span style={{color: 'var(--error-text)', fontSize: '12px'}}>
                        {`Failed to parse saved bot catalog. ${loadError}`}
                    </span>
                )}
                {validationMessages.length > 0 && (
                    <div style={{background: 'rgba(var(--error-text-color-rgb), 0.08)', borderRadius: '8px', padding: '12px'}}>
                        <strong>{'Validation'}</strong>
                        <div style={{display: 'flex', flexDirection: 'column', gap: '4px', marginTop: '8px'}}>
                            {validationMessages.map((message) => (
                                <span
                                    key={message}
                                    style={{fontSize: '12px'}}
                                >
                                    {message}
                                </span>
                            ))}
                        </div>
                    </div>
                )}
            </section>

            <div style={layoutStyle}>
                <section style={cardStyle}>
                    <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                        <strong>{'Bots'}</strong>
                        <button
                            className='btn btn-primary'
                            disabled={disabled}
                            onClick={addBot}
                            type='button'
                        >
                            {'Add bot'}
                        </button>
                    </div>

                    {bots.length === 0 && (
                        <div style={{display: 'flex', flexDirection: 'column', gap: '12px'}}>
                            <span style={{fontSize: '12px', opacity: 0.8}}>
                                {'No bots configured yet. Add your first bot or load the sample catalog.'}
                            </span>
                            <button
                                className='btn btn-secondary'
                                disabled={disabled}
                                onClick={loadSampleBots}
                                type='button'
                            >
                                {'Load sample bots'}
                            </button>
                        </div>
                    )}

                    {bots.length > 0 && (
                        <>
                            <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                                {bots.map((bot) => (
                                    <button
                                        key={bot.local_id}
                                        disabled={disabled}
                                        onClick={() => setSelectedBotId(bot.local_id)}
                                        style={botListItemStyle(bot.local_id === selectedBot?.local_id)}
                                        type='button'
                                    >
                                        <strong>{bot.display_name || bot.username || 'New bot'}</strong>
                                        <span style={{fontSize: '12px', opacity: 0.8}}>{`@${bot.username || 'username'}`}</span>
                                        <span style={{fontSize: '12px', opacity: 0.8}}>{bot.flow_id || 'No flow linked yet'}</span>
                                    </button>
                                ))}
                            </div>
                            <div style={{display: 'flex', gap: '8px', flexWrap: 'wrap'}}>
                                <button
                                    className='btn btn-secondary'
                                    disabled={disabled || !selectedBot}
                                    onClick={duplicateBot}
                                    type='button'
                                >
                                    {'Duplicate'}
                                </button>
                                <button
                                    className='btn btn-secondary'
                                    disabled={disabled || !selectedBot}
                                    onClick={removeSelectedBot}
                                    type='button'
                                >
                                    {'Remove'}
                                </button>
                            </div>
                        </>
                    )}
                </section>

                <section style={cardStyle}>
                    {!selectedBot && (
                        <span style={{fontSize: '12px', opacity: 0.8}}>
                            {'Select a bot to edit its flow binding, access policy, and input form.'}
                        </span>
                    )}

                    {selectedBot && (
                        <>
                            <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                                <strong>{selectedBot.display_name || selectedBot.username || 'Bot details'}</strong>
                                <span style={{fontSize: '12px', opacity: 0.8}}>{`Calls flow ${selectedBot.flow_id || '$FLOW_ID'}`}</span>
                            </div>

                            <div style={gridTwoStyle}>
                                <LabeledField label={'Bot ID'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, id: event.target.value}))}
                                        style={fieldStyle}
                                        value={selectedBot.id}
                                    />
                                </LabeledField>
                                <LabeledField label={'Flow ID'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, flow_id: event.target.value}))}
                                        placeholder={'thread-summary'}
                                        style={fieldStyle}
                                        value={selectedBot.flow_id}
                                    />
                                </LabeledField>
                            </div>

                            <div style={gridTwoStyle}>
                                <LabeledField label={'Bot username'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, username: sanitizeUsername(event.target.value)}))}
                                        placeholder={'thread-summary-bot'}
                                        style={fieldStyle}
                                        value={selectedBot.username}
                                    />
                                </LabeledField>
                                <LabeledField label={'Display name'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, display_name: event.target.value}))}
                                        placeholder={'Thread Summary Bot'}
                                        style={fieldStyle}
                                        value={selectedBot.display_name}
                                    />
                                </LabeledField>
                            </div>

                            <LabeledField label={'Description'}>
                                <textarea
                                    disabled={disabled}
                                    onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, description: event.target.value}))}
                                    placeholder={'Explain what this bot does in Mattermost.'}
                                    style={textAreaStyle}
                                    value={selectedBot.description}
                                />
                            </LabeledField>

                            <label style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                                <input
                                    checked={selectedBot.include_context_by_default}
                                    disabled={disabled}
                                    onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, include_context_by_default: event.target.checked}))}
                                    type='checkbox'
                                />
                                {'Include recent Mattermost context by default'}
                            </label>

                            <div style={gridTwoStyle}>
                                <LabeledField label={'Allowed teams'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_teams: splitCSV(event.target.value)}))}
                                        placeholder={'team-name, team-id'}
                                        style={fieldStyle}
                                        value={joinCSV(selectedBot.allowed_teams)}
                                    />
                                </LabeledField>
                                <LabeledField label={'Allowed channels'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_channels: splitCSV(event.target.value)}))}
                                        placeholder={'town-square, channel-id'}
                                        style={fieldStyle}
                                        value={joinCSV(selectedBot.allowed_channels)}
                                    />
                                </LabeledField>
                            </div>

                            <LabeledField label={'Allowed users'}>
                                <input
                                    disabled={disabled}
                                    onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_users: splitCSV(event.target.value)}))}
                                    placeholder={'sysadmin, user-id'}
                                    style={fieldStyle}
                                    value={joinCSV(selectedBot.allowed_users)}
                                />
                            </LabeledField>

                            <section style={{...cardStyle, padding: '12px'}}>
                                <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                                    <strong>{'Input form fields'}</strong>
                                    <button
                                        className='btn btn-secondary'
                                        disabled={disabled}
                                        onClick={() => updateBot(selectedBot.local_id, (bot) => ({...bot, input_schema: [...bot.input_schema, createEmptyInputField()]}))}
                                        type='button'
                                    >
                                        {'Add field'}
                                    </button>
                                </div>

                                {selectedBot.input_schema.length === 0 && (
                                    <span style={{fontSize: '12px', opacity: 0.8}}>
                                        {'No extra inputs. The bot will only send the main prompt to Langflow.'}
                                    </span>
                                )}

                                {selectedBot.input_schema.map((field, index) => (
                                    <div
                                        key={field.id}
                                        style={{border: '1px solid rgba(var(--center-channel-color-rgb), 0.1)', borderRadius: '10px', padding: '12px'}}
                                    >
                                        <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                                            <strong>{field.label || field.name || `Field ${index + 1}`}</strong>
                                            <button
                                                className='btn btn-secondary'
                                                disabled={disabled}
                                                onClick={() => updateBot(selectedBot.local_id, (bot) => ({
                                                    ...bot,
                                                    input_schema: bot.input_schema.filter((item) => item.id !== field.id),
                                                }))}
                                                type='button'
                                            >
                                                {'Remove'}
                                            </button>
                                        </div>

                                        <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                            <LabeledField label={'Field name'}>
                                                <input
                                                    disabled={disabled}
                                                    onChange={(event) => updateInputField(selectedBot.local_id, field.id, {name: event.target.value}, updateBot)}
                                                    placeholder={'tone'}
                                                    style={fieldStyle}
                                                    value={field.name}
                                                />
                                            </LabeledField>
                                            <LabeledField label={'Label'}>
                                                <input
                                                    disabled={disabled}
                                                    onChange={(event) => updateInputField(selectedBot.local_id, field.id, {label: event.target.value}, updateBot)}
                                                    placeholder={'Tone'}
                                                    style={fieldStyle}
                                                    value={field.label}
                                                />
                                            </LabeledField>
                                        </div>

                                        <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                            <LabeledField label={'Type'}>
                                                <select
                                                    disabled={disabled}
                                                    onChange={(event) => updateInputField(selectedBot.local_id, field.id, {type: event.target.value as InputFieldType, default_value: defaultValueForType(event.target.value as InputFieldType)}, updateBot)}
                                                    style={fieldStyle}
                                                    value={field.type}
                                                >
                                                    <option value='text'>{'Text'}</option>
                                                    <option value='textarea'>{'Textarea'}</option>
                                                    <option value='number'>{'Number'}</option>
                                                    <option value='bool'>{'Boolean'}</option>
                                                </select>
                                            </LabeledField>
                                            <LabeledField label={'Placeholder'}>
                                                <input
                                                    disabled={disabled}
                                                    onChange={(event) => updateInputField(selectedBot.local_id, field.id, {placeholder: event.target.value}, updateBot)}
                                                    placeholder={'concise'}
                                                    style={fieldStyle}
                                                    value={field.placeholder}
                                                />
                                            </LabeledField>
                                        </div>

                                        <LabeledField label={'Description'}>
                                            <input
                                                disabled={disabled}
                                                onChange={(event) => updateInputField(selectedBot.local_id, field.id, {description: event.target.value}, updateBot)}
                                                placeholder={'Optional guidance shown to users.'}
                                                style={fieldStyle}
                                                value={field.description}
                                            />
                                        </LabeledField>

                                        <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                            <LabeledField label={'Default value'}>
                                                {renderDefaultValueEditor(field, disabled, (value) => updateInputField(selectedBot.local_id, field.id, {default_value: value}, updateBot))}
                                            </LabeledField>
                                            <div style={columnStyle}>
                                                <span style={{fontWeight: 600}}>{'Required'}</span>
                                                <label style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                                                    <input
                                                        checked={field.required}
                                                        disabled={disabled}
                                                        onChange={(event) => updateInputField(selectedBot.local_id, field.id, {required: event.target.checked}, updateBot)}
                                                        type='checkbox'
                                                    />
                                                    {'Users must fill this field before running the bot'}
                                                </label>
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </section>

                            <section style={{...cardStyle, padding: '12px'}}>
                                <strong>{'Invocation preview'}</strong>
                                <div style={codeStyle}>
                                    {buildCurlPreview(selectedBot)}
                                </div>
                                <span style={{fontSize: '12px', opacity: 0.8}}>
                                    {selectedBot.username ? `Users invoke this bot with @${selectedBot.username} in channels, threads, or DM.` : 'Set a bot username to enable mention and DM routing.'}
                                </span>
                            </section>
                        </>
                    )}
                </section>
            </div>

            <details style={cardStyle}>
                <summary style={{cursor: 'pointer', fontWeight: 600}}>{'Advanced JSON preview'}</summary>
                <pre style={codeStyle}>{serializeBots(bots)}</pre>
            </details>
        </div>
    );
}

function LabeledField(props: {label: string; children: React.ReactNode}) {
    return (
        <div style={columnStyle}>
            <span style={{fontWeight: 600}}>{props.label}</span>
            {props.children}
        </div>
    );
}

function renderDefaultValueEditor(field: DraftInputField, disabled: boolean, onChange: (value: string | number | boolean) => void) {
    if (field.type === 'bool') {
        return (
            <label style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                <input
                    checked={Boolean(field.default_value)}
                    disabled={disabled}
                    onChange={(event) => onChange(event.target.checked)}
                    type='checkbox'
                />
                {'Checked by default'}
            </label>
        );
    }

    if (field.type === 'number') {
        return (
            <input
                disabled={disabled}
                onChange={(event) => onChange(Number(event.target.value || 0))}
                style={fieldStyle}
                type='number'
                value={String(field.default_value)}
            />
        );
    }

    return (
        <input
            disabled={disabled}
            onChange={(event) => onChange(event.target.value)}
            style={fieldStyle}
            type='text'
            value={String(field.default_value)}
        />
    );
}

function updateInputField(
    botLocalID: string,
    fieldID: string,
    patch: Partial<DraftInputField>,
    updateBot: (localID: string, updater: (bot: DraftBotDefinition) => DraftBotDefinition) => void,
) {
    updateBot(botLocalID, (bot) => ({
        ...bot,
        input_schema: bot.input_schema.map((field) => (
            field.id === fieldID ? {...field, ...patch} : field
        )),
    }));
}

function parseStoredBots(rawValue: unknown) {
    if (!rawValue) {
        return {bots: [] as DraftBotDefinition[], error: ''};
    }

    try {
        const parsed = typeof rawValue === 'string' ? JSON.parse(rawValue || '[]') : rawValue;
        if (!Array.isArray(parsed)) {
            return {bots: [] as DraftBotDefinition[], error: 'Saved value is not a JSON array.'};
        }
        return {
            bots: parsed.map((item) => normalizeStoredBot(item as StoredBotDefinition)),
            error: '',
        };
    } catch (error) {
        return {
            bots: [] as DraftBotDefinition[],
            error: (error as Error).message,
        };
    }
}

function normalizeStoredBot(value: StoredBotDefinition): DraftBotDefinition {
    return {
        local_id: createLocalID('bot'),
        id: stringValue(value.id),
        username: sanitizeUsername(stringValue(value.username)),
        display_name: stringValue(value.display_name),
        description: stringValue(value.description),
        flow_id: stringValue(value.flow_id),
        include_context_by_default: Boolean(value.include_context_by_default),
        allowed_teams: normalizeStringArray(value.allowed_teams),
        allowed_channels: normalizeStringArray(value.allowed_channels),
        allowed_users: normalizeStringArray(value.allowed_users),
        input_schema: Array.isArray(value.input_schema) ? value.input_schema.map(normalizeInputField) : [],
    };
}

function normalizeInputField(value: StoredInputField): DraftInputField {
    const type = normalizeInputType(stringValue(value?.type));
    return {
        id: createLocalID('input'),
        name: stringValue(value?.name),
        label: stringValue(value?.label),
        description: stringValue(value?.description),
        type,
        required: Boolean(value?.required),
        placeholder: stringValue(value?.placeholder),
        default_value: normalizeDefaultValue(type, value?.default_value),
    };
}

function serializeBots(bots: DraftBotDefinition[]) {
    return JSON.stringify(bots.map((bot) => ({
        id: bot.id.trim(),
        username: bot.username.trim(),
        display_name: bot.display_name.trim(),
        description: bot.description.trim(),
        flow_id: bot.flow_id.trim(),
        include_context_by_default: bot.include_context_by_default,
        allowed_teams: normalizeStringArray(bot.allowed_teams),
        allowed_channels: normalizeStringArray(bot.allowed_channels),
        allowed_users: normalizeStringArray(bot.allowed_users),
        input_schema: bot.input_schema.map((field) => ({
            name: field.name.trim(),
            label: field.label.trim(),
            description: field.description.trim(),
            type: field.type,
            required: field.required,
            placeholder: field.placeholder.trim(),
            default_value: field.default_value,
        })),
    })), null, 2);
}

function validateBots(bots: DraftBotDefinition[]) {
    const messages: string[] = [];
    const seenIDs = new Set<string>();
    const seenUsernames = new Set<string>();

    bots.forEach((bot, index) => {
        const label = bot.display_name || bot.username || `Bot ${index + 1}`;
        const botID = bot.id.trim();
        const username = bot.username.trim();
        const flowID = bot.flow_id.trim();

        if (!botID) {
            messages.push(`${label}: bot id is required.`);
        } else if (seenIDs.has(botID)) {
            messages.push(`${label}: bot id "${botID}" is duplicated.`);
        } else {
            seenIDs.add(botID);
        }

        if (!username) {
            messages.push(`${label}: bot username is required.`);
        } else if (seenUsernames.has(username)) {
            messages.push(`${label}: bot username "${username}" is duplicated.`);
        } else {
            seenUsernames.add(username);
        }

        if (!bot.display_name.trim()) {
            messages.push(`${label}: display name is required.`);
        }

        if (!flowID) {
            messages.push(`${label}: flow id is required.`);
        }

        const seenFields = new Set<string>();
        bot.input_schema.forEach((field, fieldIndex) => {
            const fieldLabel = field.label || field.name || `Field ${fieldIndex + 1}`;
            const fieldName = field.name.trim();
            if (!fieldName) {
                messages.push(`${label}: ${fieldLabel} is missing a field name.`);
            } else if (seenFields.has(fieldName)) {
                messages.push(`${label}: input field "${fieldName}" is duplicated.`);
            } else {
                seenFields.add(fieldName);
            }
        });
    });

    return messages;
}

function buildCurlPreview(bot: DraftBotDefinition) {
    const flowID = bot.flow_id || '$FLOW_ID';
    const username = bot.username || 'bot-username';
    return [
        `curl -X POST "$LANGFLOW_BASE_URL/api/v1/run/${flowID}" \\`,
        '  -H "Authorization: Bearer $LANGFLOW_API_KEY" \\',
        '  -H "Content-Type: application/json" \\',
        "  -d '{",
        `    "input_value": "Hello from @${username}"`,
        "  }'",
    ].join('\n');
}

function createEmptyBot(): DraftBotDefinition {
    return {
        local_id: createLocalID('bot'),
        id: '',
        username: '',
        display_name: '',
        description: '',
        flow_id: '',
        include_context_by_default: true,
        allowed_teams: [],
        allowed_channels: [],
        allowed_users: [],
        input_schema: [],
    };
}

function cloneBot(bot: DraftBotDefinition): DraftBotDefinition {
    return {
        ...bot,
        local_id: createLocalID('bot'),
        id: bot.id ? `${bot.id}-copy` : '',
        username: bot.username ? `${bot.username}-copy` : '',
        display_name: bot.display_name ? `${bot.display_name} Copy` : '',
        input_schema: bot.input_schema.map((field) => ({
            ...field,
            id: createLocalID('input'),
        })),
    };
}

function createEmptyInputField(): DraftInputField {
    return {
        id: createLocalID('input'),
        name: '',
        label: '',
        description: '',
        type: 'text',
        required: false,
        placeholder: '',
        default_value: '',
    };
}

function normalizeInputType(value: string): InputFieldType {
    if (value === 'textarea' || value === 'number' || value === 'bool') {
        return value;
    }
    return 'text';
}

function normalizeDefaultValue(type: InputFieldType, value: unknown) {
    if (type === 'bool') {
        return Boolean(value);
    }
    if (type === 'number') {
        return typeof value === 'number' ? value : Number(value || 0);
    }
    return stringValue(value);
}

function defaultValueForType(type: InputFieldType) {
    if (type === 'bool') {
        return false;
    }
    if (type === 'number') {
        return 0;
    }
    return '';
}

function splitCSV(value: string) {
    return normalizeStringArray(value.split(','));
}

function joinCSV(values: string[]) {
    return normalizeStringArray(values).join(', ');
}

function normalizeStringArray(values: unknown) {
    if (!Array.isArray(values)) {
        return [];
    }

    return values.map((item) => stringValue(item).trim()).filter(Boolean);
}

function stringValue(value: unknown) {
    if (typeof value === 'string') {
        return value;
    }
    if (value == null) {
        return '';
    }
    return String(value);
}

function sanitizeUsername(value: string) {
    return value.toLowerCase().replace(/[^a-z0-9-_]/g, '');
}

function createLocalID(prefix: string) {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
        return `${prefix}-${crypto.randomUUID()}`;
    }

    return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
