import {OpenAI, Anthropic} from '../BrandIcons';
import {Box, Checkbox, InputBase, Typography} from '@mui/material';
import {useTranslation} from 'react-i18next';

export interface ProtocolSlotData {
    url: string;
    enabled: boolean;
}

export type ProtocolKind = 'openai' | 'anthropic';

interface ProtocolSlotProps {
    kind: ProtocolKind;
    slot: ProtocolSlotData;
    onUrlChange: (url: string) => void;
    onUrlBlur: () => void;
    onToggle: () => void;
    toggleLocked?: boolean;
    helperText?: string;
    urlError?: boolean;
}

interface BrandDef {
    icon: React.ReactNode;
    labelKey: string;
    defaultLabel: string;
    placeholder: string;
}

const BRAND: Record<ProtocolKind, BrandDef> = {
    openai: {
        icon: <OpenAI size={18}/>,
        labelKey: 'providerDialog.protocol.openAILabel',
        defaultLabel: 'OpenAI Compatible',
        placeholder: 'https://api.openai.com/v1',
    },
    anthropic: {
        icon: <Anthropic size={18}/>,
        labelKey: 'providerDialog.protocol.anthropicLabel',
        defaultLabel: 'Anthropic Compatible',
        placeholder: 'https://api.anthropic.com',
    },
};

const DEFAULT_HELPERS: Record<ProtocolKind, string> = {
    openai: 'Supports models from OpenAI, Google and many other OpenAI-compatible providers',
    anthropic: 'For Anthropic-compatible AI providers, commonly used with Claude Code',
};

/**
 * A protocol slot card — brand icon, label, helper text, checkbox, and
 * an inline URL input when enabled. The entire card is one cohesive unit.
 */
const ProtocolSlot: React.FC<ProtocolSlotProps> = ({
    kind,
    slot,
    onUrlChange,
    onUrlBlur,
    onToggle,
    toggleLocked,
    helperText,
    urlError,
}) => {
    const {t} = useTranslation();
    const brand = BRAND[kind];
    const helper = helperText || DEFAULT_HELPERS[kind];
    const enabled = slot.enabled;

    return (
        <Box
            sx={{
                borderRadius: 1,
                px: 1.5,
                py: 1,
                cursor: toggleLocked ? 'not-allowed' : 'pointer',
                transition: 'all 0.15s',
                bgcolor: enabled ? 'action.selected' : 'transparent',
                '&:hover': {
                    bgcolor: toggleLocked
                        ? (enabled ? 'action.selected' : 'transparent')
                        : (enabled ? 'action.selected' : 'action.hover'),
                },
            }}
            onClick={() => { if (!toggleLocked) onToggle(); }}
        >
            {/* Header: icon + label + checkbox */}
            <Box sx={{display: 'flex', alignItems: 'flex-start', gap: 1}}>
                <Box sx={{mt: 0.2, flexShrink: 0}}>{brand.icon}</Box>
                <Box sx={{flex: 1, minWidth: 0}}>
                    <Typography variant="body2" fontWeight={500}>
                        {t(brand.labelKey, {defaultValue: brand.defaultLabel})}
                    </Typography>
                    <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{display: 'block', lineHeight: 1.3, mt: 0.15}}
                    >
                        {helper}
                    </Typography>
                </Box>
                <Checkbox
                    size="small"
                    checked={enabled}
                    disabled={toggleLocked}
                    sx={{p: 0, mt: -0.5, flexShrink: 0}}
                    onClick={(e) => e.stopPropagation()}
                    onChange={onToggle}
                />
            </Box>

            {/* Inline URL input — only when enabled */}
            {enabled && (
                <InputBase
                    fullWidth
                    size="small"
                    placeholder={brand.placeholder}
                    value={slot.url}
                    onChange={(e) => onUrlChange(e.target.value)}
                    onBlur={onUrlBlur}
                    error={urlError}
                    onClick={(e) => e.stopPropagation()}
                    sx={{
                        mt: 1.25,
                        px: 1.5,
                        py: 0.75,
                        fontSize: '0.8rem',
                        fontFamily: 'monospace',
                        color: 'primary.main',
                        bgcolor: 'background.default',
                        borderRadius: 0.75,
                        border: urlError ? 1 : '1px solid transparent',
                        borderColor: urlError ? 'error.main' : 'divider',
                        '&:hover': {borderColor: 'text.disabled'},
                        '&:focus-within': {
                            borderColor: 'primary.main',
                            boxShadow: '0 0 0 1px rgba(25,118,210,0.12)',
                        },
                    }}
                />
            )}
        </Box>
    );
};

export default ProtocolSlot;
