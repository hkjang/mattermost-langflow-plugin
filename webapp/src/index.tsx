import manifest from 'manifest';
import React from 'react';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import {setSiteURL} from './client';
import BotDefinitionsSetting from './components/bot_definitions_setting';
import PluginErrorBoundary from './components/error_boundary';
import RHSPane from './components/rhs';
import StatusPanel from './components/status_panel';
import type {PluginRegistry} from './types/mattermost-webapp';

const LangflowTitle = () => {
    return (
        <span style={{display: 'inline-flex', alignItems: 'center', gap: '8px'}}>
            <span style={badgeStyle}>{'LF'}</span>
            {'Langflow'}
        </span>
    );
};

const badgeStyle: React.CSSProperties = {
    alignItems: 'center',
    background: 'var(--button-bg)',
    borderRadius: '999px',
    color: 'var(--button-color)',
    display: 'inline-flex',
    fontSize: '11px',
    fontWeight: 700,
    height: '22px',
    justifyContent: 'center',
    width: '22px',
};

const HeaderIcon = () => <span style={badgeStyle}>{'LF'}</span>;

const SafeBotDefinitionsSetting = (props: React.ComponentProps<typeof BotDefinitionsSetting>) => (
    <PluginErrorBoundary area={'봇 설정'}>
        <BotDefinitionsSetting {...props}/>
    </PluginErrorBoundary>
);

const SafeStatusPanel = () => (
    <PluginErrorBoundary area={'상태 패널'}>
        <StatusPanel/>
    </PluginErrorBoundary>
);

const SafeRHSPane = () => (
    <PluginErrorBoundary area={'Langflow 사이드바'}>
        <RHSPane/>
    </PluginErrorBoundary>
);

export default class Plugin {
    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        let siteURL = store.getState().entities.general.config.SiteURL;
        if (!siteURL) {
            siteURL = window.location.origin;
        }
        setSiteURL(siteURL);

        if (registry.registerAdminConsoleCustomSetting) {
            registry.registerAdminConsoleCustomSetting('BotDefinitions', SafeBotDefinitionsSetting, {showTitle: true});
            registry.registerAdminConsoleCustomSetting('StatusPanel', SafeStatusPanel, {showTitle: true});
        }

        if (registry.registerRightHandSidebarComponent) {
            const rhs = registry.registerRightHandSidebarComponent(SafeRHSPane, LangflowTitle);
            registry.registerChannelHeaderButtonAction(
                <HeaderIcon/>,
                () => store.dispatch(rhs.toggleRHSPlugin as any),
                'Langflow',
                'Langflow 열기',
            );
        }
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
