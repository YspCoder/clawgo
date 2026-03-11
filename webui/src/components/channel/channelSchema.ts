import { Check, KeyRound, ListFilter, Smartphone, Users } from 'lucide-react';

export type ChannelKey = 'telegram' | 'whatsapp' | 'discord' | 'feishu' | 'qq' | 'dingtalk' | 'maixcam';

export type ChannelField =
  | { key: string; type: 'text' | 'password' | 'number'; placeholder?: string }
  | { key: string; type: 'boolean' }
  | { key: string; type: 'list'; placeholder?: string };

export type ChannelDefinition = {
  id: ChannelKey;
  titleKey: string;
  hintKey: string;
  sections: Array<{
    id: string;
    titleKey: string;
    hintKey: string;
    fields: ChannelField[];
    columns?: 1 | 2;
  }>;
};

export const channelDefinitions: Record<ChannelKey, ChannelDefinition> = {
  telegram: {
    id: 'telegram',
    titleKey: 'telegram',
    hintKey: 'telegramChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'token', type: 'password' },
          { key: 'streaming', type: 'boolean' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 2,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: '123456789' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  whatsapp: {
    id: 'whatsapp',
    titleKey: 'whatsappBridge',
    hintKey: 'whatsappBridgeHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'bridge_url', type: 'text' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: '8613012345678@s.whatsapp.net' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  discord: {
    id: 'discord',
    titleKey: 'discord',
    hintKey: 'discordChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'token', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: 'discord-user-id' },
        ],
      },
    ],
  },
  feishu: {
    id: 'feishu',
    titleKey: 'feishu',
    hintKey: 'feishuChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'app_id', type: 'text' },
          { key: 'app_secret', type: 'password' },
          { key: 'encrypt_key', type: 'password' },
          { key: 'verification_token', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 2,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  qq: {
    id: 'qq',
    titleKey: 'qq',
    hintKey: 'qqChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'app_id', type: 'text' },
          { key: 'app_secret', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
    ],
  },
  dingtalk: {
    id: 'dingtalk',
    titleKey: 'dingtalk',
    hintKey: 'dingtalkChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'client_id', type: 'text' },
          { key: 'client_secret', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
    ],
  },
  maixcam: {
    id: 'maixcam',
    titleKey: 'maixcam',
    hintKey: 'maixcamChannelHint',
    sections: [
      {
        id: 'network',
        titleKey: 'channelSectionNetwork',
        hintKey: 'channelSectionNetworkHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'host', type: 'text' },
          { key: 'port', type: 'number' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
    ],
  },
};

export function parseChannelList(text: string) {
  return String(text || '')
    .split('\n')
    .map((line) => line.split(','))
    .flat()
    .map((item) => item.trim())
    .filter(Boolean);
}

function getWhatsAppFieldDescription(t: (key: string) => string, fieldKey: string) {
  switch (fieldKey) {
    case 'enabled':
      return t('whatsappFieldEnabledHint');
    case 'bridge_url':
      return t('whatsappFieldBridgeURLHint');
    case 'allow_from':
      return t('whatsappFieldAllowFromHint');
    case 'enable_groups':
      return t('whatsappFieldEnableGroupsHint');
    case 'require_mention_in_groups':
      return t('whatsappFieldRequireMentionHint');
    default:
      return '';
  }
}

export function getChannelFieldDescription(t: (key: string) => string, channelKey: ChannelKey, fieldKey: string) {
  if (channelKey === 'whatsapp') return getWhatsAppFieldDescription(t, fieldKey);
  const map: Partial<Record<ChannelKey, Partial<Record<string, string>>>> = {
    telegram: {
      enabled: 'channelFieldTelegramEnabledHint',
      token: 'channelFieldTelegramTokenHint',
      streaming: 'channelFieldTelegramStreamingHint',
      allow_from: 'channelFieldTelegramAllowFromHint',
      allow_chats: 'channelFieldTelegramAllowChatsHint',
      enable_groups: 'channelFieldEnableGroupsHint',
      require_mention_in_groups: 'channelFieldRequireMentionHint',
    },
    discord: {
      enabled: 'channelFieldDiscordEnabledHint',
      token: 'channelFieldDiscordTokenHint',
      allow_from: 'channelFieldDiscordAllowFromHint',
    },
    feishu: {
      enabled: 'channelFieldFeishuEnabledHint',
      app_id: 'channelFieldFeishuAppIDHint',
      app_secret: 'channelFieldFeishuAppSecretHint',
      encrypt_key: 'channelFieldFeishuEncryptKeyHint',
      verification_token: 'channelFieldFeishuVerificationTokenHint',
      allow_from: 'channelFieldFeishuAllowFromHint',
      allow_chats: 'channelFieldFeishuAllowChatsHint',
      enable_groups: 'channelFieldEnableGroupsHint',
      require_mention_in_groups: 'channelFieldRequireMentionHint',
    },
    qq: {
      enabled: 'channelFieldQQEnabledHint',
      app_id: 'channelFieldQQAppIDHint',
      app_secret: 'channelFieldQQAppSecretHint',
      allow_from: 'channelFieldQQAllowFromHint',
    },
    dingtalk: {
      enabled: 'channelFieldDingTalkEnabledHint',
      client_id: 'channelFieldDingTalkClientIDHint',
      client_secret: 'channelFieldDingTalkClientSecretHint',
      allow_from: 'channelFieldDingTalkAllowFromHint',
    },
    maixcam: {
      enabled: 'channelFieldMaixCamEnabledHint',
      host: 'channelFieldMaixCamHostHint',
      port: 'channelFieldMaixCamPortHint',
      allow_from: 'channelFieldMaixCamAllowFromHint',
    },
  };
  const key = map[channelKey]?.[fieldKey];
  return key ? t(key) : '';
}

export function getChannelSectionIcon(sectionID: string) {
  switch (sectionID) {
    case 'connection':
      return KeyRound;
    case 'access':
      return ListFilter;
    case 'groups':
      return Users;
    case 'network':
      return Smartphone;
    default:
      return Check;
  }
}
