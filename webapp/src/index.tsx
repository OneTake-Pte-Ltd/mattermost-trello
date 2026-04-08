// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import manifest from 'manifest';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import type {PluginRegistry} from 'types/mattermost-webapp';

import BotConfigSetting from './components/bot_config_setting';

export default class Plugin {
    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        // Register the custom CRUD component for the BotConfigurations setting.
        // This replaces the raw JSON textarea with a user-friendly bot management UI.
        registry.registerAdminConsoleCustomSetting('BotConfigurations', BotConfigSetting, {showTitle: true});
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
