import React, {useState, useCallback} from 'react';

type BotConfigEntry = {
    botUsername: string;
    botDisplayName: string;
    trelloApiKey: string;
    trelloApiToken: string;
    trelloBoardId: string;
    trelloListId: string;
    botContext: string;
    allowedUsers: string[]; // stored as array in JSON; edited as comma-separated string in UI
};

type Props = {
    id: string;
    value: string;
    onChange: (id: string, value: string) => void;
    disabled?: boolean;
};

const emptyBot = (): BotConfigEntry => ({
    botUsername: '',
    botDisplayName: '',
    trelloApiKey: '',
    trelloApiToken: '',
    trelloBoardId: '',
    trelloListId: '',
    botContext: '',
    allowedUsers: [],
});

const parseBots = (raw: string): BotConfigEntry[] => {
    try {
        const parsed = JSON.parse(raw || '[]');
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
};

const serializeBots = (bots: BotConfigEntry[]): string => JSON.stringify(bots);

const allowedUsersToString = (users: string[]): string =>
    (users || []).join(', ');

const stringToAllowedUsers = (s: string): string[] =>
    s.split(',').map((u) => u.trim()).filter(Boolean);

// Inline styles that match Mattermost admin panel aesthetics without external CSS
const styles: Record<string, React.CSSProperties> = {
    container: {padding: '8px 0'},
    botCard: {
        border: '1px solid #e0e0e0',
        borderRadius: '4px',
        marginBottom: '12px',
        overflow: 'hidden',
    },
    botHeader: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '10px 14px',
        backgroundColor: '#f4f4f8',
        cursor: 'pointer',
        userSelect: 'none',
    },
    botHeaderTitle: {
        fontWeight: 600,
        fontSize: '14px',
        color: '#3d3c40',
    },
    botHeaderActions: {display: 'flex', alignItems: 'center', gap: '8px'},
    botBody: {padding: '14px 16px'},
    formRow: {marginBottom: '12px'},
    label: {
        display: 'block',
        fontWeight: 600,
        fontSize: '12px',
        marginBottom: '4px',
        color: '#3d3c40',
    },
    hint: {fontSize: '11px', color: '#888', marginTop: '2px'},
    addButton: {marginTop: '4px'},
    emptyHint: {color: '#888', fontSize: '13px', marginBottom: '12px'},
};

const Field: React.FC<{
    label: string;
    hint?: string;
    type?: string;
    value: string;
    placeholder?: string;
    disabled?: boolean;
    onChange: (v: string) => void;
    textarea?: boolean;
}> = ({label, hint, type = 'text', value, placeholder, disabled, onChange, textarea}) => (
    <div style={styles.formRow}>
        <label style={styles.label}>{label}</label>
        {textarea ? (
            <textarea
                className='form-control'
                value={value}
                placeholder={placeholder}
                disabled={disabled}
                rows={3}
                onChange={(e) => onChange(e.target.value)}
            />
        ) : (
            <input
                type={type}
                className='form-control'
                value={value}
                placeholder={placeholder}
                disabled={disabled}
                onChange={(e) => onChange(e.target.value)}
            />
        )}
        {hint && <p style={styles.hint}>{hint}</p>}
    </div>
);

const BotConfigSetting: React.FC<Props> = ({id, value, onChange, disabled}) => {
    const [bots, setBots] = useState<BotConfigEntry[]>(() => parseBots(value));
    const [expanded, setExpanded] = useState<Record<number, boolean>>({});

    const save = useCallback(
        (newBots: BotConfigEntry[]) => {
            setBots(newBots);
            onChange(id, serializeBots(newBots));
        },
        [id, onChange],
    );

    const toggleExpand = (idx: number) =>
        setExpanded((prev) => ({...prev, [idx]: !prev[idx]}));

    const addBot = () => {
        const newBots = [...bots, emptyBot()];
        save(newBots);
        setExpanded((prev) => ({...prev, [newBots.length - 1]: true}));
    };

    const removeBot = (idx: number) => {
        const newBots = bots.filter((_, i) => i !== idx);
        save(newBots);
        setExpanded((prev) => {
            const next: Record<number, boolean> = {};
            Object.entries(prev).forEach(([k, v]) => {
                const ki = parseInt(k, 10);
                if (ki < idx) {
                    next[ki] = v;
                } else if (ki > idx) {
                    next[ki - 1] = v;
                }
            });
            return next;
        });
    };

    const updateField = <K extends keyof BotConfigEntry>(
        idx: number,
        field: K,
        val: BotConfigEntry[K],
    ) => {
        const newBots = bots.map((b, i) => (i === idx ? {...b, [field]: val} : b));
        save(newBots);
    };

    return (
        <div style={styles.container}>
            {bots.length === 0 && (
                <p style={styles.emptyHint}>
                    No bots configured yet. Click <strong>Add Bot</strong> to create one.
                </p>
            )}

            {bots.map((bot, idx) => {
                const isOpen = expanded[idx] ?? false;
                const headerLabel = bot.botUsername
                    ? `@${bot.botUsername}${bot.botDisplayName ? ` — ${bot.botDisplayName}` : ''}`
                    : `Bot ${idx + 1} (unsaved)`;

                return (
                    <div
                        key={idx}
                        style={styles.botCard}
                    >
                        <div
                            style={styles.botHeader}
                            onClick={() => toggleExpand(idx)}
                        >
                            <span style={styles.botHeaderTitle}>
                                {isOpen ? '▾' : '▸'}&nbsp;{headerLabel}
                            </span>
                            <div style={styles.botHeaderActions}>
                                <button
                                    className='btn btn-danger btn-sm'
                                    disabled={disabled}
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        removeBot(idx);
                                    }}
                                >
                                    {'Remove'}
                                </button>
                            </div>
                        </div>

                        {isOpen && (
                            <div style={styles.botBody}>
                                <Field
                                    label='Bot Username *'
                                    hint='The Mattermost username for this bot (e.g. trellobot). Users will mention @username to invoke it.'
                                    value={bot.botUsername}
                                    placeholder='trellobot'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'botUsername', v)}
                                />
                                <Field
                                    label='Display Name *'
                                    hint="Human-readable name shown in Mattermost (e.g. Trello Bot)."
                                    value={bot.botDisplayName}
                                    placeholder='Trello Bot'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'botDisplayName', v)}
                                />
                                <Field
                                    label='Trello API Key *'
                                    hint='Your Trello Power-Up API key from https://trello.com/power-ups/admin'
                                    value={bot.trelloApiKey}
                                    placeholder='Trello API key'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'trelloApiKey', v)}
                                />
                                <Field
                                    label='Trello API Token *'
                                    hint='Your Trello API token (generated from the Power-Up admin page).'
                                    type='password'
                                    value={bot.trelloApiToken}
                                    placeholder='Trello API token'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'trelloApiToken', v)}
                                />
                                <Field
                                    label='Trello Board ID *'
                                    hint='The ID of the Trello board this bot will create cards on.'
                                    value={bot.trelloBoardId}
                                    placeholder='Trello board ID'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'trelloBoardId', v)}
                                />
                                <Field
                                    label='Trello List ID *'
                                    hint='The ID of the Trello list where new cards will be created.'
                                    value={bot.trelloListId}
                                    placeholder='Trello list ID'
                                    disabled={disabled}
                                    onChange={(v) => updateField(idx, 'trelloListId', v)}
                                />
                                <Field
                                    label='Bot Context'
                                    hint="Optional. Describe this bot's role, personality, or domain. Appended to the global context in every Anthropic call made by this bot."
                                    value={bot.botContext}
                                    placeholder='e.g. This bot handles engineering bugs. Be concise and technical.'
                                    disabled={disabled}
                                    textarea={true}
                                    onChange={(v) => updateField(idx, 'botContext', v)}
                                />
                                <Field
                                    label='Allowed Users'
                                    hint='Optional. Comma-separated Mattermost usernames that are permitted to use this bot. Leave blank to allow everyone. Example: alice, bob, @ceo'
                                    value={allowedUsersToString(bot.allowedUsers)}
                                    placeholder='alice, bob, ceo'
                                    disabled={disabled}
                                    onChange={(v) =>
                                        updateField(idx, 'allowedUsers', stringToAllowedUsers(v))
                                    }
                                />
                            </div>
                        )}
                    </div>
                );
            })}

            <button
                className='btn btn-primary'
                style={styles.addButton}
                disabled={disabled}
                onClick={addBot}
            >
                {'+ Add Bot'}
            </button>
        </div>
    );
};

export default BotConfigSetting;
