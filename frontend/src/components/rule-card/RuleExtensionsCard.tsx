import { Add as AddIcon, Extension as ExtensionIcon } from '@mui/icons-material';
import { Box, Chip, IconButton, Stack, Tooltip, Typography } from '@mui/material';
import { styled } from '@mui/material/styles';
import React from 'react';
import type { FlagSpec, RuleFlags } from '@/components/RoutingGraphTypes';

const CARD_STYLES = {
    width: 180,
    minHeight: 120,
    padding: 10,
} as const;

const StyledExtensionsCard = styled(Box, {
    shouldForwardProp: (prop) => prop !== 'active',
})<{ active: boolean }>(({ active, theme }) => ({
    display: 'flex',
    flexDirection: 'column',
    padding: CARD_STYLES.padding,
    borderRadius: theme.shape.borderRadius,
    border: '1px dashed',
    borderColor: theme.palette.divider,
    backgroundColor: theme.palette.background.paper,
    width: CARD_STYLES.width,
    minHeight: CARD_STYLES.minHeight,
    boxShadow: theme.shadows[1],
    opacity: active ? 1 : 0.6,
    transition: 'all 0.2s ease-in-out',
}));

export interface RuleExtensionsCardProps {
    flags?: RuleFlags;
    registry?: FlagSpec[];
    active: boolean;
    onOpenCatalog: () => void;
    onToggleFlag?: (key: string) => void;
}

const flagBoolValue = (flags: RuleFlags | undefined, key: string): boolean => {
    if (!flags) return false;
    switch (key) {
        case 'cursor_compat':
            return !!flags.cursorCompat;
        case 'cursor_compat_auto':
            return !!flags.cursorCompatAuto;
        case 'skip_usage':
            return !!flags.skipUsage;
        case 'use_max_completion_tokens':
            return !!flags.useMaxCompletionTokens;
        default:
            return false;
    }
};

const flagStringValue = (flags: RuleFlags | undefined, key: string): string => {
    if (!flags) return '';
    switch (key) {
        case 'custom_user_agent':
            return flags.customUserAgent || '';
        default:
            return '';
    }
};

/**
 * RuleExtensionsCard renders a compact card displaying the rule's enabled
 * extension flags. The "+ Add" action opens the catalog dialog where users
 * pick which flags to enable and supply any parameters they require.
 */
export const RuleExtensionsCard: React.FC<RuleExtensionsCardProps> = ({
    flags,
    registry,
    active,
    onOpenCatalog,
    onToggleFlag,
}) => {
    const enabled = (registry || []).filter((spec) => {
        if (spec.type === 'bool') return flagBoolValue(flags, spec.key);
        return flagStringValue(flags, spec.key) !== '';
    });

    return (
        <StyledExtensionsCard active={active}>
            <Stack direction="row" alignItems="center" spacing={0.5} sx={{ mb: 0.5 }}>
                <ExtensionIcon sx={{ fontSize: 14, color: 'text.secondary' }} />
                <Typography variant="caption" sx={{ fontWeight: 600, color: 'text.secondary', flexGrow: 1 }}>
                    Extensions
                </Typography>
                <Tooltip title="Configure rule extensions">
                    <IconButton size="small" onClick={onOpenCatalog} sx={{ p: 0.25 }}>
                        <AddIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                </Tooltip>
            </Stack>

            {enabled.length === 0 ? (
                <Box
                    onClick={onOpenCatalog}
                    sx={{
                        flexGrow: 1,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        color: 'text.disabled',
                        fontSize: '0.7rem',
                        cursor: 'pointer',
                        textAlign: 'center',
                        px: 0.5,
                    }}
                >
                    None enabled. Click to configure.
                </Box>
            ) : (
                <Stack direction="row" flexWrap="wrap" gap={0.5} sx={{ mt: 0.25 }}>
                    {enabled.map((spec) => {
                        const isString = spec.type === 'string';
                        const stringVal = isString ? flagStringValue(flags, spec.key) : '';
                        const label = isString && stringVal ? `${spec.label}: ${stringVal}` : spec.label;
                        const title = isString && stringVal
                            ? `${spec.description}\nValue: ${stringVal}`
                            : spec.description;
                        return (
                            <Tooltip key={spec.key} title={title}>
                                <Chip
                                    size="small"
                                    label={label}
                                    color="primary"
                                    variant="outlined"
                                    onClick={onOpenCatalog}
                                    onDelete={
                                        spec.type === 'bool' && onToggleFlag
                                            ? () => onToggleFlag(spec.key)
                                            : undefined
                                    }
                                    sx={{ maxWidth: '100%', fontSize: '0.65rem', height: 20 }}
                                />
                            </Tooltip>
                        );
                    })}
                </Stack>
            )}
        </StyledExtensionsCard>
    );
};

export default RuleExtensionsCard;
