import { CheckCircle } from '@mui/icons-material';
import { Box, Card, CardContent, CircularProgress, Typography, alpha } from '@mui/material';

interface ModelCardProps {
    model: string;
    isSelected: boolean;
    onClick: () => void;
    variant?: 'standard' | 'starred';
    gridColumns?: number;
    loading?: boolean;
    showNewBadge?: boolean;
}

export default function ModelCard({
    model,
    isSelected,
    onClick,
    variant = 'standard',
    gridColumns,
    loading = false,
    showNewBadge = false,
}: ModelCardProps) {
    const getCardStyles = () => {
        const baseStyles = {
            width: '100%',
            height: 60,
            border: 1,
            borderRadius: 1,
            cursor: loading ? 'wait' : 'pointer',
            transition: 'border-color 0.16s ease, background-color 0.16s ease',
            position: 'relative' as const,
            boxShadow: 'none',
            '&:hover': loading ? {} : {
                borderColor: 'primary.main',
                backgroundColor: (theme) => alpha(theme.palette.primary.main, 0.04),
            },
        };

        if (variant === 'starred') {
            return {
                ...baseStyles,
                borderColor: isSelected ? 'primary.main' : 'warning.main',
                backgroundColor: isSelected ? 'primary.50' : 'warning.50',
                '&:hover': {
                    borderColor: 'primary.main',
                    backgroundColor: (theme) => alpha(theme.palette.primary.main, 0.04),
                },
            };
        }

        return {
            ...baseStyles,
            borderColor: isSelected ? 'primary.main' : 'grey.300',
            backgroundColor: isSelected ? 'primary.50' : 'background.paper',
            '&:hover': {
                borderColor: 'primary.main',
                backgroundColor: (theme) => alpha(theme.palette.primary.main, 0.04),
            },
        };
    };

    return (
        <Card sx={getCardStyles()} onClick={loading ? undefined : onClick}>
            <CardContent sx={{
                py: 1,
                px: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                height: '100%',
                '&:last-child': {
                    pb: 1,
                }
            }}>
                {loading ? (
                    <CircularProgress size={20} />
                ) : (
                    <Typography
                        variant="body2"
                        sx={{
                            fontWeight: 500,
                            fontSize: '0.8rem',
                            lineHeight: 1.2,
                            wordBreak: 'break-word',
                            textAlign: 'center',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            width: '100%',
                        }}
                    >
                        {model}
                    </Typography>
                )}
                {isSelected && !loading && (
                    <CheckCircle
                        color="primary"
                        sx={{
                            position: 'absolute',
                            top: 4,
                            right: 4,
                            fontSize: 16
                        }}
                    />
                )}
                {showNewBadge && !loading && (
                    <Box
                        sx={{
                            position: 'absolute',
                            top: 4,
                            left: 4,
                            bgcolor: 'success.main',
                            color: 'white',
                            fontSize: '0.6rem',
                            px: 0.5,
                            py: 0.2,
                            borderRadius: 1,
                            fontWeight: 'bold',
                        }}
                    >
                        NEW
                    </Box>
                )}
            </CardContent>
        </Card>
    );
}
