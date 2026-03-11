import manifest from 'manifest';
import React from 'react';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import {setSiteURL} from './client';
import BotDefinitionsSetting from './components/bot_definitions_setting';
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

export default class Plugin {
    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        let siteURL = store.getState().entities.general.config.SiteURL;
        if (!siteURL) {
            siteURL = window.location.origin;
        }
        setSiteURL(siteURL);

        if (registry.registerAdminConsoleCustomSetting) {
            registry.registerAdminConsoleCustomSetting('BotDefinitions', BotDefinitionsSetting, {showTitle: true});
            registry.registerAdminConsoleCustomSetting('StatusPanel', StatusPanel, {showTitle: true});
        }

        if (registry.registerRightHandSidebarComponent) {
            const rhs = registry.registerRightHandSidebarComponent(RHSPane, LangflowTitle);
            registry.registerChannelHeaderButtonAction(
                <HeaderIcon/>,
                () => store.dispatch(rhs.toggleRHSPlugin as any),
                'Langflow',
                'Open Langflow',
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
