import manifest from 'manifest';
import React, {useEffect, useMemo, useRef, useState} from 'react';

import type {
    AdminPluginConfig,
    BotDefinition,
    ConnectionStatus,
    ManagedBotStatus,
    PluginStatus,
} from '../client';
import {getAdminConfig, getStatus, testConnection} from '../client';

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

type DraftPluginConfig = {
    service: {
        base_url: string;
        auth_mode: string;
        auth_token: string;
        allow_hosts: string;
    };
    runtime: {
        default_timeout_seconds: number;
        enable_streaming: boolean;
        streaming_update_ms: number;
        max_input_length: number;
        max_output_length: number;
        context_post_limit: number;
        enable_debug_logs: boolean;
        enable_usage_logs: boolean;
    };
    bots: DraftBotDefinition[];
};

type CustomSettingProps = {
    id?: string;
    value?: unknown;
    disabled?: boolean;
    setByEnv?: boolean;
    helpText?: React.ReactNode;
    onChange: (id: string, value: unknown) => void;
    setSaveNeeded?: () => void;
};

const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '20px',
};

const sectionStyle: React.CSSProperties = {
    background: 'white',
    border: '1px solid rgba(63, 67, 80, 0.12)',
    borderRadius: '8px',
    boxShadow: '0 2px 3px rgba(0, 0, 0, 0.08)',
    display: 'flex',
    flexDirection: 'column',
    gap: '16px',
    padding: '24px',
};

const sectionHeaderStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '4px',
};

const titleRowStyle: React.CSSProperties = {
    alignItems: 'center',
    display: 'flex',
    gap: '10px',
    justifyContent: 'space-between',
};

const sectionTitleStyle: React.CSSProperties = {
    fontSize: '16px',
    fontWeight: 600,
};

const sectionSubtitleStyle: React.CSSProperties = {
    color: 'rgba(63, 67, 80, 0.72)',
    fontSize: '14px',
};

const fieldStyle: React.CSSProperties = {
    border: '1px solid rgba(63, 67, 80, 0.16)',
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

const botLayoutStyle: React.CSSProperties = {
    display: 'grid',
    gap: '16px',
    gridTemplateColumns: '320px minmax(0, 1fr)',
};

const botListItemStyle = (selected: boolean): React.CSSProperties => ({
    background: selected ? 'rgba(var(--button-bg-rgb), 0.10)' : 'rgba(63, 67, 80, 0.03)',
    border: `1px solid ${selected ? 'rgba(var(--button-bg-rgb), 0.30)' : 'rgba(63, 67, 80, 0.10)'}`,
    borderRadius: '10px',
    cursor: 'pointer',
    display: 'flex',
    flexDirection: 'column',
    gap: '4px',
    padding: '12px',
    textAlign: 'left',
    width: '100%',
});

const infoBoxStyle: React.CSSProperties = {
    background: 'rgba(var(--button-bg-rgb), 0.08)',
    border: '1px solid rgba(var(--button-bg-rgb), 0.18)',
    borderRadius: '8px',
    display: 'flex',
    flexDirection: 'column',
    gap: '6px',
    padding: '12px',
};

const warningBoxStyle: React.CSSProperties = {
    background: 'rgba(var(--error-text-color-rgb), 0.08)',
    border: '1px solid rgba(var(--error-text-color-rgb), 0.18)',
    borderRadius: '8px',
    display: 'flex',
    flexDirection: 'column',
    gap: '6px',
    padding: '12px',
};

const codeStyle: React.CSSProperties = {
    background: 'rgba(63, 67, 80, 0.04)',
    borderRadius: '8px',
    fontFamily: 'monospace',
    fontSize: '12px',
    padding: '12px',
    whiteSpace: 'pre-wrap',
};

const versionBadgeStyle: React.CSSProperties = {
    background: 'rgba(var(--button-bg-rgb), 0.10)',
    border: '1px solid rgba(var(--button-bg-rgb), 0.18)',
    borderRadius: '999px',
    color: 'var(--center-channel-color)',
    fontSize: '12px',
    fontWeight: 700,
    padding: '4px 10px',
    whiteSpace: 'nowrap',
};

const sampleBots: BotDefinition[] = [
    {
        id: 'thread-summary-bot',
        username: 'thread-summary-bot',
        display_name: '스레드 요약 봇',
        description: '현재 스레드를 요약하고 액션 아이템을 정리합니다.',
        flow_id: 'thread-summary',
        include_context_by_default: true,
        allowed_teams: ['engineering'],
        allowed_channels: ['town-square'],
        allowed_users: [],
        input_schema: [
            {
                name: 'tone',
                label: '톤',
                type: 'text',
                placeholder: '간결하게',
                default_value: '간결하게',
            },
        ],
    },
    {
        id: 'support-assistant-bot',
        username: 'support-assistant-bot',
        display_name: '지원 도우미',
        description: 'Langflow의 고객지원 flow를 호출하는 봇입니다.',
        flow_id: 'support-assistant',
        include_context_by_default: true,
        allowed_teams: [],
        allowed_channels: [],
        allowed_users: [],
        input_schema: [],
    },
];

export default function ConfigSetting(props: CustomSettingProps) {
    const settingKey = props.id || 'Config';
    const disabled = Boolean(props.disabled || props.setByEnv);

    const [config, setConfig] = useState<DraftPluginConfig>(createDefaultConfig());
    const [selectedBotID, setSelectedBotID] = useState('');
    const [status, setStatus] = useState<PluginStatus | null>(null);
    const [connection, setConnection] = useState<ConnectionStatus | null>(null);
    const [source, setSource] = useState('config');
    const [loadError, setLoadError] = useState('');
    const [loadingConfig, setLoadingConfig] = useState(true);
    const [loadingStatus, setLoadingStatus] = useState(true);
    const [testingConnection, setTestingConnection] = useState(false);
    const lastSubmittedValueRef = useRef('');

    useEffect(() => {
        let cancelled = false;

        async function loadConfig() {
            setLoadingConfig(true);
            setLoadError('');

            const serializedValue = serializeSettingValue(props.value);
            if (serializedValue && serializedValue === lastSubmittedValueRef.current) {
                if (!cancelled) {
                    setLoadingConfig(false);
                }
                return;
            }

            const parsedValue = parseStoredConfigValue(props.value);
            if (parsedValue.ok) {
                if (!cancelled) {
                    setConfig(parsedValue.config);
                    setSource('config');
                    setSelectedBotID((current) => pickSelectedBotID(parsedValue.config.bots, current));
                    lastSubmittedValueRef.current = serializedValue;
                    setLoadingConfig(false);
                }
                return;
            }

            try {
                const response = await getAdminConfig();
                if (cancelled) {
                    return;
                }
                const nextConfig = normalizeAdminConfig(response.config);
                setConfig(nextConfig);
                setSource(response.source || 'config');
                setSelectedBotID((current) => pickSelectedBotID(nextConfig.bots, current));
                lastSubmittedValueRef.current = serializeSettingValue(buildStoredConfig(nextConfig));
            } catch (error) {
                if (!cancelled) {
                    setLoadError((error as Error).message);
                }
            } finally {
                if (!cancelled) {
                    setLoadingConfig(false);
                }
            }
        }

        loadConfig();

        return () => {
            cancelled = true;
        };
    }, [props.value]);

    useEffect(() => {
        let cancelled = false;

        async function loadStatus() {
            setLoadingStatus(true);
            try {
                const pluginStatus = await getStatus();
                if (!cancelled) {
                    setStatus(pluginStatus);
                }
            } catch (error) {
                if (!cancelled) {
                    setLoadError((error as Error).message);
                }
            } finally {
                if (!cancelled) {
                    setLoadingStatus(false);
                }
            }
        }

        loadStatus();

        return () => {
            cancelled = true;
        };
    }, []);

    const selectedBot = useMemo(
        () => config.bots.find((bot) => bot.local_id === selectedBotID) || config.bots[0] || null,
        [config.bots, selectedBotID],
    );

    const validationMessages = useMemo(() => validateConfig(config), [config]);

    const applyConfig = (nextConfig: DraftPluginConfig, nextSelectedBotID?: string) => {
        setConfig(nextConfig);
        const nextValue = JSON.stringify(buildStoredConfig(nextConfig), null, 2);
        lastSubmittedValueRef.current = nextValue;
        props.onChange(settingKey, nextValue);
        props.setSaveNeeded?.();

        if (nextConfig.bots.length === 0) {
            setSelectedBotID('');
            return;
        }

        if (nextSelectedBotID) {
            setSelectedBotID(nextSelectedBotID);
            return;
        }

        setSelectedBotID((current) => pickSelectedBotID(nextConfig.bots, current));
    };

    const updateService = (patch: Partial<DraftPluginConfig['service']>) => {
        applyConfig({
            ...config,
            service: {
                ...config.service,
                ...patch,
            },
        });
    };

    const updateRuntime = (patch: Partial<DraftPluginConfig['runtime']>) => {
        applyConfig({
            ...config,
            runtime: {
                ...config.runtime,
                ...patch,
            },
        });
    };

    const updateBot = (localID: string, updater: (bot: DraftBotDefinition) => DraftBotDefinition) => {
        applyConfig({
            ...config,
            bots: config.bots.map((bot) => (bot.local_id === localID ? updater(bot) : bot)),
        }, localID);
    };

    const addBot = () => {
        const nextBot = createEmptyBot();
        applyConfig({...config, bots: [...config.bots, nextBot]}, nextBot.local_id);
    };

    const duplicateBot = () => {
        if (!selectedBot) {
            return;
        }
        const duplicate = cloneBot(selectedBot);
        applyConfig({...config, bots: [...config.bots, duplicate]}, duplicate.local_id);
    };

    const removeSelectedBot = () => {
        if (!selectedBot) {
            return;
        }
        applyConfig({...config, bots: config.bots.filter((bot) => bot.local_id !== selectedBot.local_id)});
    };

    const loadSampleBots = () => {
        const nextBots = sampleBots.map(normalizeStoredBot);
        applyConfig({...config, bots: nextBots}, nextBots[0]?.local_id);
    };

    const runConnectionTest = async () => {
        setTestingConnection(true);
        setConnection(null);
        try {
            setConnection(await testConnection());
        } catch (error) {
            setLoadError((error as Error).message);
        } finally {
            setTestingConnection(false);
        }
    };

    return (
        <div style={containerStyle}>
            <section style={sectionStyle}>
                <div style={sectionHeaderStyle}>
                    <div style={titleRowStyle}>
                        <div style={sectionTitleStyle}>{'Langflow 설정'}</div>
                        <span style={versionBadgeStyle}>{`플러그인 버전 ${manifest.version}`}</span>
                    </div>
                    <div style={sectionSubtitleStyle}>
                        {'Agents 플러그인처럼 서비스 연결과 봇 카탈로그를 한 화면에서 관리합니다. 봇 1개는 Langflow flow 1개에 고정 연결됩니다.'}
                    </div>
                </div>
                <div style={infoBoxStyle}>
                    <span>{'설정을 저장하면 플러그인이 Mattermost 봇 계정을 자동 생성 또는 갱신합니다.'}</span>
                    <span>{'사용자는 해당 봇과 DM을 하거나 채널에서 @멘션하면 연결된 Langflow run API가 호출됩니다.'}</span>
                    <span>{'봇 내부 식별자는 username을 기준으로 자동 생성되며, 별도 Bot ID 입력은 필요하지 않습니다.'}</span>
                </div>
                {source === 'legacy' && (
                    <div style={warningBoxStyle}>
                        <strong>{'기존 설정을 불러왔습니다.'}</strong>
                        <span>{'현재 값은 예전 개별 설정 항목에서 읽어 왔습니다. 이번에 저장하면 Agents 스타일의 단일 Config 형식으로 자동 마이그레이션됩니다.'}</span>
                    </div>
                )}
                {props.setByEnv && (
                    <div style={warningBoxStyle}>
                        <span>{'이 설정은 환경 변수로 관리되고 있어 여기에서 수정할 수 없습니다.'}</span>
                    </div>
                )}
                {props.helpText}
                {loadError && (
                    <div style={warningBoxStyle}>
                        <span>{loadError}</span>
                    </div>
                )}
                {validationMessages.length > 0 && (
                    <div style={warningBoxStyle}>
                        <strong>{'검증 결과'}</strong>
                        {validationMessages.map((message) => (
                            <span key={message}>{message}</span>
                        ))}
                    </div>
                )}
            </section>

            <section style={sectionStyle}>
                <div style={sectionHeaderStyle}>
                    <div style={sectionTitleStyle}>{'서비스 연결'}</div>
                    <div style={sectionSubtitleStyle}>
                        {'Langflow 서버 주소와 인증 방식을 지정합니다. 이 값은 모든 봇이 공통으로 사용합니다.'}
                    </div>
                </div>
                {loadingConfig ? <span>{'설정을 불러오는 중입니다...'}</span> : (
                    <>
                        <div style={gridTwoStyle}>
                            <LabeledField label={'Langflow 기본 URL'}>
                                <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateService({base_url: event.target.value})}
                                        placeholder={'https://langflow.example.com 또는 https://gateway.example.com/langflow'}
                                        style={fieldStyle}
                                        value={config.service.base_url}
                                    />
                                    <span style={{fontSize: '12px', opacity: 0.8}}>
                                        {'플러그인이 /api/v1/run/{flow_id}를 자동으로 붙입니다. 루트 URL, 서브경로 URL, /api 또는 /api/v1 URL 모두 입력할 수 있습니다.'}
                                    </span>
                                </div>
                            </LabeledField>
                            <LabeledField label={'인증 헤더 방식'}>
                                <select
                                    disabled={disabled}
                                    onChange={(event) => updateService({auth_mode: event.target.value})}
                                    style={fieldStyle}
                                    value={config.service.auth_mode}
                                >
                                    <option value='bearer'>{'Bearer 토큰'}</option>
                                    <option value='x-api-key'>{'x-api-key'}</option>
                                </select>
                            </LabeledField>
                        </div>
                        <div style={gridTwoStyle}>
                            <LabeledField label={'Langflow API 토큰'}>
                                <input
                                    disabled={disabled}
                                    onChange={(event) => updateService({auth_token: event.target.value})}
                                    placeholder={'토큰을 입력하세요'}
                                    style={fieldStyle}
                                    type='password'
                                    value={config.service.auth_token}
                                />
                            </LabeledField>
                            <LabeledField label={'허용 호스트'}>
                                <input
                                    disabled={disabled}
                                    onChange={(event) => updateService({allow_hosts: event.target.value})}
                                    placeholder={'langflow.example.com, *.internal.example.com'}
                                    style={fieldStyle}
                                    value={config.service.allow_hosts}
                                />
                            </LabeledField>
                        </div>
                    </>
                )}
            </section>

            <section style={sectionStyle}>
                <div style={sectionHeaderStyle}>
                    <div style={sectionTitleStyle}>{'Langflow 봇'}</div>
                    <div style={sectionSubtitleStyle}>
                        {'봇 1개가 Langflow flow 1개를 호출합니다. 각 봇마다 접근 대상과 추가 입력 폼을 다르게 지정할 수 있습니다.'}
                    </div>
                </div>

                <div style={botLayoutStyle}>
                    <div style={{display: 'flex', flexDirection: 'column', gap: '12px'}}>
                        <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                            <strong>{'봇 목록'}</strong>
                            <button
                                className='btn btn-primary'
                                disabled={disabled}
                                onClick={addBot}
                                type='button'
                            >
                                {'봇 추가'}
                            </button>
                        </div>

                        {config.bots.length === 0 && (
                            <div style={{display: 'flex', flexDirection: 'column', gap: '12px'}}>
                                <span style={{fontSize: '12px', opacity: 0.8}}>
                                    {'아직 등록된 봇이 없습니다. 첫 번째 봇을 추가하거나 예시 구성을 불러오세요.'}
                                </span>
                                <button
                                    className='btn btn-secondary'
                                    disabled={disabled}
                                    onClick={loadSampleBots}
                                    type='button'
                                >
                                    {'예시 봇 불러오기'}
                                </button>
                            </div>
                        )}

                        {config.bots.length > 0 && (
                            <>
                                <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                                    {config.bots.map((bot) => (
                                        <button
                                            key={bot.local_id}
                                            disabled={disabled}
                                            onClick={() => setSelectedBotID(bot.local_id)}
                                            style={botListItemStyle(bot.local_id === selectedBot?.local_id)}
                                            type='button'
                                        >
                                            <strong>{bot.display_name || bot.username || '새 봇'}</strong>
                                            <span style={{fontSize: '12px', opacity: 0.8}}>{`@${bot.username || 'username'}`}</span>
                                            <span style={{fontSize: '12px', opacity: 0.8}}>{bot.flow_id || 'flow 미지정'}</span>
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
                                        {'복제'}
                                    </button>
                                    <button
                                        className='btn btn-secondary'
                                        disabled={disabled || !selectedBot}
                                        onClick={removeSelectedBot}
                                        type='button'
                                    >
                                        {'삭제'}
                                    </button>
                                </div>
                            </>
                        )}
                    </div>

                    <div style={{display: 'flex', flexDirection: 'column', gap: '16px'}}>
                        {!selectedBot && (
                            <span style={{fontSize: '12px', opacity: 0.8}}>
                                {'왼쪽에서 봇을 선택하면 flow 연결, 접근 정책, 입력 필드를 수정할 수 있습니다.'}
                            </span>
                        )}

                        {selectedBot && (
                            <>
                                <div style={infoBoxStyle}>
                                    <strong>{selectedBot.display_name || selectedBot.username || '봇 상세 설정'}</strong>
                                    <span>{`이 봇의 내부 식별자는 username(@${selectedBot.username || 'username'})을 기준으로 자동 생성됩니다.`}</span>
                                    <span>{'Mattermost의 실제 bot user id는 저장 후 자동으로 생성되며 아래 상태 섹션에서 확인할 수 있습니다.'}</span>
                                </div>

                                <div style={gridTwoStyle}>
                                    <LabeledField label={'봇 사용자 이름'}>
                                        <input
                                            disabled={disabled}
                                            onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, username: sanitizeUsername(event.target.value)}))}
                                            placeholder={'thread-summary-bot'}
                                            style={fieldStyle}
                                            value={selectedBot.username}
                                        />
                                    </LabeledField>
                                    <LabeledField label={'표시 이름'}>
                                        <input
                                            disabled={disabled}
                                            onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, display_name: event.target.value}))}
                                            placeholder={'스레드 요약 봇'}
                                            style={fieldStyle}
                                            value={selectedBot.display_name}
                                        />
                                    </LabeledField>
                                </div>

                                <div style={gridTwoStyle}>
                                    <LabeledField label={'연결할 Flow ID'}>
                                        <input
                                            disabled={disabled}
                                            onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, flow_id: event.target.value}))}
                                            placeholder={'thread-summary'}
                                            style={fieldStyle}
                                            value={selectedBot.flow_id}
                                        />
                                    </LabeledField>
                                    <LabeledField label={'컨텍스트 포함'}>
                                        <label style={{display: 'flex', gap: '8px', alignItems: 'center', minHeight: '40px'}}>
                                            <input
                                                checked={selectedBot.include_context_by_default}
                                                disabled={disabled}
                                                onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, include_context_by_default: event.target.checked}))}
                                                type='checkbox'
                                            />
                                            {'최근 Mattermost 대화를 기본 컨텍스트로 포함'}
                                        </label>
                                    </LabeledField>
                                </div>

                                <LabeledField label={'설명'}>
                                    <textarea
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, description: event.target.value}))}
                                        placeholder={'이 봇이 Mattermost에서 어떤 일을 하는지 설명하세요.'}
                                        style={textAreaStyle}
                                        value={selectedBot.description}
                                    />
                                </LabeledField>

                                <div style={gridTwoStyle}>
                                    <LabeledField label={'허용 팀'}>
                                        <input
                                            disabled={disabled}
                                            onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_teams: splitCSV(event.target.value)}))}
                                            placeholder={'team-name, team-id'}
                                            style={fieldStyle}
                                            value={joinCSV(selectedBot.allowed_teams)}
                                        />
                                    </LabeledField>
                                    <LabeledField label={'허용 채널'}>
                                        <input
                                            disabled={disabled}
                                            onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_channels: splitCSV(event.target.value)}))}
                                            placeholder={'town-square, channel-id'}
                                            style={fieldStyle}
                                            value={joinCSV(selectedBot.allowed_channels)}
                                        />
                                    </LabeledField>
                                </div>

                                <LabeledField label={'허용 사용자'}>
                                    <input
                                        disabled={disabled}
                                        onChange={(event) => updateBot(selectedBot.local_id, (bot) => ({...bot, allowed_users: splitCSV(event.target.value)}))}
                                        placeholder={'sysadmin, user-id'}
                                        style={fieldStyle}
                                        value={joinCSV(selectedBot.allowed_users)}
                                    />
                                </LabeledField>

                                <div style={{...infoBoxStyle, background: 'rgba(63, 67, 80, 0.04)', border: '1px solid rgba(63, 67, 80, 0.10)'}}>
                                    <strong>{'호출 미리보기'}</strong>
                                    <pre style={codeStyle}>{buildCurlPreview(config, selectedBot)}</pre>
                                </div>

                                <section style={{...sectionStyle, padding: '16px'}}>
                                    <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                                        <strong>{'추가 입력 필드'}</strong>
                                        <button
                                            className='btn btn-secondary'
                                            disabled={disabled}
                                            onClick={() => updateBot(selectedBot.local_id, (bot) => ({...bot, input_schema: [...bot.input_schema, createEmptyInputField()]}))}
                                            type='button'
                                        >
                                            {'필드 추가'}
                                        </button>
                                    </div>

                                    {selectedBot.input_schema.length === 0 && (
                                        <span style={{fontSize: '12px', opacity: 0.8}}>
                                            {'추가 입력 필드가 없으면 사용자의 메인 프롬프트만 Langflow로 전송됩니다.'}
                                        </span>
                                    )}

                                    {selectedBot.input_schema.map((field, index) => (
                                        <div
                                            key={field.id}
                                            style={{border: '1px solid rgba(63, 67, 80, 0.10)', borderRadius: '10px', padding: '12px'}}
                                        >
                                            <div style={{display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'center'}}>
                                                <strong>{field.label || field.name || `필드 ${index + 1}`}</strong>
                                                <button
                                                    className='btn btn-secondary'
                                                    disabled={disabled}
                                                    onClick={() => updateBot(selectedBot.local_id, (bot) => ({
                                                        ...bot,
                                                        input_schema: bot.input_schema.filter((item) => item.id !== field.id),
                                                    }))}
                                                    type='button'
                                                >
                                                    {'삭제'}
                                                </button>
                                            </div>

                                            <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                                <LabeledField label={'필드 이름'}>
                                                    <input
                                                        disabled={disabled}
                                                        onChange={(event) => updateInputField(selectedBot.local_id, field.id, {name: event.target.value}, updateBot)}
                                                        placeholder={'tone'}
                                                        style={fieldStyle}
                                                        value={field.name}
                                                    />
                                                </LabeledField>
                                                <LabeledField label={'표시 라벨'}>
                                                    <input
                                                        disabled={disabled}
                                                        onChange={(event) => updateInputField(selectedBot.local_id, field.id, {label: event.target.value}, updateBot)}
                                                        placeholder={'톤'}
                                                        style={fieldStyle}
                                                        value={field.label}
                                                    />
                                                </LabeledField>
                                            </div>

                                            <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                                <LabeledField label={'타입'}>
                                                    <select
                                                        disabled={disabled}
                                                        onChange={(event) => updateInputField(
                                                            selectedBot.local_id,
                                                            field.id,
                                                            {
                                                                type: event.target.value as InputFieldType,
                                                                default_value: defaultValueForType(event.target.value as InputFieldType),
                                                            },
                                                            updateBot,
                                                        )}
                                                        style={fieldStyle}
                                                        value={field.type}
                                                    >
                                                        <option value='text'>{'텍스트'}</option>
                                                        <option value='textarea'>{'여러 줄 텍스트'}</option>
                                                        <option value='number'>{'숫자'}</option>
                                                        <option value='bool'>{'불리언'}</option>
                                                    </select>
                                                </LabeledField>
                                                <LabeledField label={'플레이스홀더'}>
                                                    <input
                                                        disabled={disabled}
                                                        onChange={(event) => updateInputField(selectedBot.local_id, field.id, {placeholder: event.target.value}, updateBot)}
                                                        placeholder={'간결하게'}
                                                        style={fieldStyle}
                                                        value={field.placeholder}
                                                    />
                                                </LabeledField>
                                            </div>

                                            <LabeledField label={'설명'}>
                                                <input
                                                    disabled={disabled}
                                                    onChange={(event) => updateInputField(selectedBot.local_id, field.id, {description: event.target.value}, updateBot)}
                                                    placeholder={'사용자에게 보여 줄 안내 문구입니다.'}
                                                    style={fieldStyle}
                                                    value={field.description}
                                                />
                                            </LabeledField>

                                            <div style={{...gridTwoStyle, marginTop: '12px'}}>
                                                <LabeledField label={'기본값'}>
                                                    {renderDefaultValueEditor(
                                                        field,
                                                        disabled,
                                                        (value) => updateInputField(selectedBot.local_id, field.id, {default_value: value}, updateBot),
                                                    )}
                                                </LabeledField>
                                                <LabeledField label={'필수 여부'}>
                                                    <label style={{display: 'flex', gap: '8px', alignItems: 'center', minHeight: '40px'}}>
                                                        <input
                                                            checked={field.required}
                                                            disabled={disabled}
                                                            onChange={(event) => updateInputField(selectedBot.local_id, field.id, {required: event.target.checked}, updateBot)}
                                                            type='checkbox'
                                                        />
                                                        {'실행 전에 반드시 입력해야 합니다'}
                                                    </label>
                                                </LabeledField>
                                            </div>
                                        </div>
                                    ))}
                                </section>
                            </>
                        )}
                    </div>
                </div>
            </section>

            <section style={sectionStyle}>
                <div style={sectionHeaderStyle}>
                    <div style={sectionTitleStyle}>{'실행 정책'}</div>
                    <div style={sectionSubtitleStyle}>
                        {'스트리밍, 길이 제한, 로그 정책 등 공통 실행 옵션을 관리합니다.'}
                    </div>
                </div>
                <div style={gridTwoStyle}>
                    <LabeledField label={'기본 타임아웃(초)'}>
                        <input
                            disabled={disabled}
                            onChange={(event) => updateRuntime({default_timeout_seconds: parseNumber(event.target.value, 30)})}
                            style={fieldStyle}
                            type='number'
                            value={String(config.runtime.default_timeout_seconds)}
                        />
                    </LabeledField>
                    <LabeledField label={'스트리밍 갱신 주기(ms)'}>
                        <input
                            disabled={disabled}
                            onChange={(event) => updateRuntime({streaming_update_ms: parseNumber(event.target.value, 350)})}
                            style={fieldStyle}
                            type='number'
                            value={String(config.runtime.streaming_update_ms)}
                        />
                    </LabeledField>
                </div>
                <div style={gridTwoStyle}>
                    <LabeledField label={'최대 입력 길이'}>
                        <input
                            disabled={disabled}
                            onChange={(event) => updateRuntime({max_input_length: parseNumber(event.target.value, 4000)})}
                            style={fieldStyle}
                            type='number'
                            value={String(config.runtime.max_input_length)}
                        />
                    </LabeledField>
                    <LabeledField label={'최대 출력 길이'}>
                        <input
                            disabled={disabled}
                            onChange={(event) => updateRuntime({max_output_length: parseNumber(event.target.value, 8000)})}
                            style={fieldStyle}
                            type='number'
                            value={String(config.runtime.max_output_length)}
                        />
                    </LabeledField>
                </div>
                <div style={gridTwoStyle}>
                    <LabeledField label={'컨텍스트 포스트 수 제한'}>
                        <input
                            disabled={disabled}
                            onChange={(event) => updateRuntime({context_post_limit: parseNumber(event.target.value, 8)})}
                            style={fieldStyle}
                            type='number'
                            value={String(config.runtime.context_post_limit)}
                        />
                    </LabeledField>
                    <LabeledField label={'스트리밍 응답'}>
                        <label style={{display: 'flex', gap: '8px', alignItems: 'center', minHeight: '40px'}}>
                            <input
                                checked={config.runtime.enable_streaming}
                                disabled={disabled}
                                onChange={(event) => updateRuntime({enable_streaming: event.target.checked})}
                                type='checkbox'
                            />
                            {'Langflow를 ?stream=true로 호출'}
                        </label>
                    </LabeledField>
                </div>
                <div style={gridTwoStyle}>
                    <LabeledField label={'디버그 로그'}>
                        <label style={{display: 'flex', gap: '8px', alignItems: 'center', minHeight: '40px'}}>
                            <input
                                checked={config.runtime.enable_debug_logs}
                                disabled={disabled}
                                onChange={(event) => updateRuntime({enable_debug_logs: event.target.checked})}
                                type='checkbox'
                            />
                            {'상세 진단 로그 기록'}
                        </label>
                    </LabeledField>
                    <LabeledField label={'사용 로그'}>
                        <label style={{display: 'flex', gap: '8px', alignItems: 'center', minHeight: '40px'}}>
                            <input
                                checked={config.runtime.enable_usage_logs}
                                disabled={disabled}
                                onChange={(event) => updateRuntime({enable_usage_logs: event.target.checked})}
                                type='checkbox'
                            />
                            {'실행 이력 로그 기록'}
                        </label>
                    </LabeledField>
                </div>
            </section>

            <section style={sectionStyle}>
                <div style={sectionHeaderStyle}>
                    <div style={sectionTitleStyle}>{'연결 상태'}</div>
                    <div style={sectionSubtitleStyle}>
                        {'저장된 설정을 기준으로 봇 생성 여부와 Langflow 연결 상태를 확인합니다.'}
                    </div>
                </div>
                {loadingStatus && <span>{'상태를 불러오는 중입니다...'}</span>}
                {!loadingStatus && status && (
                    <>
                        <div style={gridTwoStyle}>
                            <StatusField
                                label={'기본 URL'}
                                value={status.base_url || '설정되지 않음'}
                            />
                            <StatusField
                                label={'설정된 봇 수'}
                                value={String(status.bot_count)}
                            />
                            <StatusField
                                label={'허용 호스트'}
                                value={(status.allow_hosts || []).join(', ') || '기본 URL 호스트 사용'}
                            />
                            <StatusField
                                label={'스트리밍'}
                                value={status.streaming_enabled ? '사용' : '사용 안 함'}
                            />
                        </div>
                        {status.config_error && (
                            <div style={warningBoxStyle}>
                                <span>{`설정 오류: ${status.config_error}`}</span>
                            </div>
                        )}
                        {status.bot_sync?.last_error && (
                            <div style={warningBoxStyle}>
                                <span>{`봇 동기화 오류: ${status.bot_sync.last_error}`}</span>
                            </div>
                        )}
                        {(status.bots || []).length > 0 && (
                            <div style={{display: 'flex', flexDirection: 'column', gap: '10px'}}>
                                {(status.bots || []).map((bot) => {
                                    const managed = (status.managed_bots || []).find((item) => item.bot_id === bot.id);
                                    return (
                                        <div
                                            key={bot.id}
                                            style={{border: '1px solid rgba(63, 67, 80, 0.10)', borderRadius: '10px', display: 'flex', flexDirection: 'column', gap: '4px', padding: '12px'}}
                                        >
                                            <strong>{bot.display_name || bot.username}</strong>
                                            <span>{`@${bot.username} -> ${bot.flow_id}`}</span>
                                            <span>{managedBotSummary(managed)}</span>
                                            {managed?.status_message && <span>{`상태 메모: ${managed.status_message}`}</span>}
                                            {bot.description && <span>{bot.description}</span>}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                        <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                            <button
                                className='btn btn-primary'
                                disabled={testingConnection}
                                onClick={runConnectionTest}
                                type='button'
                            >
                                {testingConnection ? '연결 확인 중...' : '연결 테스트'}
                            </button>
                            <span style={{fontSize: '12px', opacity: 0.8}}>
                                {'저장 후 봇이 생성되지 않았으면 이 섹션의 상태 메모와 서버 로그를 함께 확인하세요.'}
                            </span>
                        </div>
                        {connection && (
                            <div style={infoBoxStyle}>
                                <strong>{connection.ok ? '연결에 성공했습니다.' : '연결에 실패했습니다.'}</strong>
                                <span>{connection.url}</span>
                                <span style={{whiteSpace: 'pre-wrap'}}>{connection.message || '응답 메시지가 없습니다.'}</span>
                                {connection.error_code && <span>{`오류 코드: ${connection.error_code}`}</span>}
                                {connection.detail && <span style={{whiteSpace: 'pre-wrap'}}>{`상세: ${connection.detail}`}</span>}
                                {connection.hint && <span style={{whiteSpace: 'pre-wrap'}}>{`조치: ${connection.hint}`}</span>}
                                {connection.retryable !== undefined && <span>{`재시도 가능: ${connection.retryable ? '예' : '아니오'}`}</span>}
                            </div>
                        )}
                    </>
                )}
            </section>

            <details style={sectionStyle}>
                <summary style={{cursor: 'pointer', fontWeight: 600}}>{'고급 JSON 미리보기'}</summary>
                <pre style={codeStyle}>{JSON.stringify(buildStoredConfig(config), null, 2)}</pre>
            </details>
        </div>
    );
}

function LabeledField(props: {label: string; children: React.ReactNode}) {
    return (
        <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
            <span style={{fontWeight: 600}}>{props.label}</span>
            {props.children}
        </div>
    );
}

function StatusField(props: {label: string; value: string}) {
    return (
        <div style={infoBoxStyle}>
            <strong>{props.label}</strong>
            <span>{props.value}</span>
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
                {'기본값으로 체크됨'}
            </label>
        );
    }

    if (field.type === 'number') {
        return (
            <input
                disabled={disabled}
                onChange={(event) => onChange(parseNumber(event.target.value, 0))}
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

function createDefaultConfig(): DraftPluginConfig {
    return {
        service: {
            base_url: '',
            auth_mode: 'bearer',
            auth_token: '',
            allow_hosts: '',
        },
        runtime: {
            default_timeout_seconds: 30,
            enable_streaming: true,
            streaming_update_ms: 350,
            max_input_length: 4000,
            max_output_length: 8000,
            context_post_limit: 8,
            enable_debug_logs: false,
            enable_usage_logs: true,
        },
        bots: [],
    };
}

function parseStoredConfigValue(value: unknown) {
    if (value == null || value === '') {
        return {ok: false, config: createDefaultConfig()};
    }

    try {
        const parsed = typeof value === 'string' ? JSON.parse(value) : value;
        return {ok: true, config: normalizeAdminConfig(parsed as AdminPluginConfig)};
    } catch {
        return {ok: false, config: createDefaultConfig()};
    }
}

function normalizeAdminConfig(value?: AdminPluginConfig): DraftPluginConfig {
    const next = createDefaultConfig();
    if (!value) {
        return next;
    }

    next.service.base_url = stringValue(value.service?.base_url);
    next.service.auth_mode = normalizeAuthMode(stringValue(value.service?.auth_mode));
    next.service.auth_token = stringValue(value.service?.auth_token);
    next.service.allow_hosts = stringValue(value.service?.allow_hosts);

    next.runtime.default_timeout_seconds = parseNumber(value.runtime?.default_timeout_seconds, 30);
    next.runtime.enable_streaming = value.runtime?.enable_streaming ?? true;
    next.runtime.streaming_update_ms = parseNumber(value.runtime?.streaming_update_ms, 350);
    next.runtime.max_input_length = parseNumber(value.runtime?.max_input_length, 4000);
    next.runtime.max_output_length = parseNumber(value.runtime?.max_output_length, 8000);
    next.runtime.context_post_limit = parseNumber(value.runtime?.context_post_limit, 8);
    next.runtime.enable_debug_logs = Boolean(value.runtime?.enable_debug_logs);
    next.runtime.enable_usage_logs = value.runtime?.enable_usage_logs ?? true;
    next.bots = Array.isArray(value.bots) ? value.bots.map((bot, index) => normalizeStoredBot(bot, index)) : [];

    return next;
}

function buildStoredConfig(config: DraftPluginConfig): AdminPluginConfig {
    return {
        service: {
            base_url: config.service.base_url.trim(),
            auth_mode: normalizeAuthMode(config.service.auth_mode),
            auth_token: config.service.auth_token.trim(),
            allow_hosts: config.service.allow_hosts.trim(),
        },
        runtime: {
            default_timeout_seconds: parseNumber(config.runtime.default_timeout_seconds, 30),
            enable_streaming: Boolean(config.runtime.enable_streaming),
            streaming_update_ms: parseNumber(config.runtime.streaming_update_ms, 350),
            max_input_length: parseNumber(config.runtime.max_input_length, 4000),
            max_output_length: parseNumber(config.runtime.max_output_length, 8000),
            context_post_limit: parseNumber(config.runtime.context_post_limit, 8),
            enable_debug_logs: Boolean(config.runtime.enable_debug_logs),
            enable_usage_logs: Boolean(config.runtime.enable_usage_logs),
        },
        bots: config.bots.map((bot) => ({
            id: bot.username.trim(),
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
        })),
    };
}

function normalizeStoredBot(value: Partial<BotDefinition>, index = 0): DraftBotDefinition {
    return {
        local_id: createIndexedLocalID('bot', index),
        username: sanitizeUsername(stringValue(value.username)),
        display_name: stringValue(value.display_name),
        description: stringValue(value.description),
        flow_id: stringValue(value.flow_id),
        include_context_by_default: value.include_context_by_default ?? true,
        allowed_teams: normalizeStringArray(value.allowed_teams),
        allowed_channels: normalizeStringArray(value.allowed_channels),
        allowed_users: normalizeStringArray(value.allowed_users),
        input_schema: Array.isArray(value.input_schema) ? value.input_schema.map((field, fieldIndex) => normalizeInputField(field, fieldIndex)) : [],
    };
}

function normalizeInputField(value: NonNullable<BotDefinition['input_schema']>[number], index = 0): DraftInputField {
    const type = normalizeInputType(stringValue(value?.type));
    return {
        id: createIndexedLocalID('input', index),
        name: stringValue(value?.name),
        label: stringValue(value?.label),
        description: stringValue(value?.description),
        type,
        required: Boolean(value?.required),
        placeholder: stringValue(value?.placeholder),
        default_value: normalizeDefaultValue(type, value?.default_value),
    };
}

function validateConfig(config: DraftPluginConfig) {
    const messages: string[] = [];
    const seenUsernames = new Set<string>();

    if (!config.service.base_url.trim()) {
        messages.push('서비스 연결: Langflow 기본 URL은 필수입니다.');
    }

    config.bots.forEach((bot, index) => {
        const label = bot.display_name || bot.username || `봇 ${index + 1}`;
        const username = bot.username.trim();
        const flowID = bot.flow_id.trim();

        if (!username) {
            messages.push(`${label}: 봇 username은 필수입니다.`);
        } else if (seenUsernames.has(username)) {
            messages.push(`${label}: 봇 username "${username}"이 중복되었습니다.`);
        } else {
            seenUsernames.add(username);
        }

        if (!bot.display_name.trim()) {
            messages.push(`${label}: 표시 이름은 필수입니다.`);
        }

        if (!flowID) {
            messages.push(`${label}: flow ID는 필수입니다.`);
        }

        const seenFields = new Set<string>();
        bot.input_schema.forEach((field, fieldIndex) => {
            const fieldLabel = field.label || field.name || `필드 ${fieldIndex + 1}`;
            const fieldName = field.name.trim();
            if (!fieldName) {
                messages.push(`${label}: ${fieldLabel}에 필드 이름이 없습니다.`);
            } else if (seenFields.has(fieldName)) {
                messages.push(`${label}: 입력 필드 "${fieldName}"가 중복되었습니다.`);
            } else {
                seenFields.add(fieldName);
            }
        });
    });

    return messages;
}

function buildCurlPreview(config: DraftPluginConfig, bot: DraftBotDefinition) {
    const flowID = bot.flow_id || '$FLOW_ID';
    const header = config.service.auth_mode === 'x-api-key' ?
        '  -H "x-api-key: $LANGFLOW_API_KEY" \\' :
        '  -H "Authorization: Bearer $LANGFLOW_API_KEY" \\';

    return [
        `curl -X POST "${config.service.base_url || '$LANGFLOW_BASE_URL'}/api/v1/run/${flowID}?stream=true" \\`,
        header,
        '  -H "Content-Type: application/json" \\',
        "  -d '{",
        `    "input_value": "Hello from @${bot.username || 'bot-username'}",`,
        '    "session_id": "mattermost:bot-username:thread-or-channel:user-id"',
        "  }'",
    ].join('\n');
}

function createEmptyBot(): DraftBotDefinition {
    return {
        local_id: createLocalID('bot'),
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
        username: bot.username ? `${bot.username}-copy` : '',
        display_name: bot.display_name ? `${bot.display_name} 복사본` : '',
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

function normalizeAuthMode(value: string) {
    return value === 'x-api-key' ? 'x-api-key' : 'bearer';
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
        return parseNumber(value, 0);
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

function pickSelectedBotID(bots: DraftBotDefinition[], current: string) {
    if (current && bots.some((bot) => bot.local_id === current)) {
        return current;
    }
    return bots[0]?.local_id || '';
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

function parseNumber(value: unknown, fallback: number) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed <= 0) {
        return fallback;
    }
    return parsed;
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

function createIndexedLocalID(prefix: string, index: number) {
    return `${prefix}-${index}`;
}

function serializeSettingValue(value: unknown) {
    if (value == null || value === '') {
        return '';
    }
    if (typeof value === 'string') {
        return value;
    }
    try {
        return JSON.stringify(value);
    } catch {
        return '';
    }
}

function managedBotSummary(bot?: ManagedBotStatus) {
    if (!bot) {
        return '저장 후 아직 동기화되지 않았습니다.';
    }

    const pieces = [
        `Mattermost 사용자 ID: ${bot.user_id || '생성 대기 중'}`,
        `플러그인 관리: ${bot.registered ? '예' : '아니오'}`,
        `활성 상태: ${bot.active ? '예' : '아니오'}`,
    ];
    return pieces.join(' | ');
}
