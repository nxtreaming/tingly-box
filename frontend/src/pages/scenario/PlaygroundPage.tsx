import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Alert,
    Box,
    Button,
    Card,
    CardContent,
    CircularProgress,
    FormControl,
    InputLabel,
    MenuItem,
    Select,
    Stack,
    TextField,
    Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { api } from '@/services/api';
import PageLayout from '@/components/PageLayout';
import UnifiedCard from '@/components/UnifiedCard';
import CardGrid from '@/components/CardGrid';

const IMAGE_SCENARIO = 'imagegen';

const extractModelsFromRules = (rules: any[] | undefined | null): string[] => {
    if (!Array.isArray(rules)) return [];
    const seen = new Set<string>();
    rules.forEach((r) => {
        if (r?.disabled) return;
        const name = r?.request_model;
        if (typeof name === 'string' && name.trim()) {
            seen.add(name.trim());
        }
    });
    return Array.from(seen);
};

const PlaygroundPage: React.FC = () => {
    const { t } = useTranslation();

    const [models, setModels] = useState<string[]>([]);
    const [model, setModel] = useState<string>('');
    const [prompt, setPrompt] = useState<string>('');
    const [size, setSize] = useState<string>('1024x1024');
    const [count, setCount] = useState<number>(1);
    const [results, setResults] = useState<{ url?: string; b64_json?: string }[]>([]);
    const [sending, setSending] = useState(false);
    const [error, setError] = useState<string>('');
    const [loadingModels, setLoadingModels] = useState(false);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            setLoadingModels(true);
            const resp = await api.getRules(IMAGE_SCENARIO);
            const rules = Array.isArray(resp?.data) ? resp.data : (Array.isArray(resp) ? resp : []);
            const list = extractModelsFromRules(rules);
            if (!cancelled) {
                setModels(list);
                setModel((current) => current || list[0] || '');
                setLoadingModels(false);
            }
        })();
        return () => { cancelled = true; };
    }, []);

    const handleGenerate = useCallback(async () => {
        if (!prompt.trim() || !model) return;
        setSending(true);
        setError('');
        setResults([]);
        try {
            const resp = await api.playgroundImageGenerate(IMAGE_SCENARIO, {
                model,
                prompt: prompt.trim(),
                n: count,
                size,
            });
            if (resp?.error) {
                setError(resp.error.message || JSON.stringify(resp.error));
            } else if (Array.isArray(resp?.data)) {
                setResults(resp.data);
            } else {
                setError('Unexpected response shape');
            }
        } catch (err: any) {
            setError(err?.message || 'Request failed');
        } finally {
            setSending(false);
        }
    }, [prompt, model, count, size]);

    const noModels = useMemo(() => models.length === 0, [models]);

    return (
        <PageLayout loading={false}>
            <CardGrid>
                <UnifiedCard
                    size="full"
                    title={t('playground.imageTitle', { defaultValue: 'Image Generation Playground' })}
                >
                    <Stack spacing={2}>
                        {error && (
                            <Alert severity="error" onClose={() => setError('')}>
                                {error}
                            </Alert>
                        )}

                        {noModels && !loadingModels && (
                            <Alert severity="info">
                                {t('playground.noImageModels', {
                                    defaultValue: 'No image generation rules configured. Add one on the Image Gen page first.',
                                })}
                            </Alert>
                        )}

                        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
                            <FormControl size="small" sx={{ minWidth: 240 }}>
                                <InputLabel id="image-model-label">
                                    {t('playground.model', { defaultValue: 'Model' })}
                                </InputLabel>
                                <Select
                                    labelId="image-model-label"
                                    label={t('playground.model', { defaultValue: 'Model' })}
                                    value={model}
                                    onChange={(e) => setModel(e.target.value)}
                                    disabled={noModels}
                                >
                                    {models.map((m) => (
                                        <MenuItem key={m} value={m}>{m}</MenuItem>
                                    ))}
                                </Select>
                            </FormControl>
                            <FormControl size="small" sx={{ minWidth: 140 }}>
                                <InputLabel id="image-size-label">
                                    {t('playground.size', { defaultValue: 'Size' })}
                                </InputLabel>
                                <Select
                                    labelId="image-size-label"
                                    label={t('playground.size', { defaultValue: 'Size' })}
                                    value={size}
                                    onChange={(e) => setSize(e.target.value)}
                                >
                                    <MenuItem value="256x256">256x256</MenuItem>
                                    <MenuItem value="512x512">512x512</MenuItem>
                                    <MenuItem value="1024x1024">1024x1024</MenuItem>
                                    <MenuItem value="1024x1792">1024x1792</MenuItem>
                                    <MenuItem value="1792x1024">1792x1024</MenuItem>
                                </Select>
                            </FormControl>
                            <TextField
                                size="small"
                                type="number"
                                label={t('playground.count', { defaultValue: 'N' })}
                                value={count}
                                onChange={(e) => {
                                    const n = Number(e.target.value);
                                    setCount(Number.isFinite(n) && n > 0 ? Math.min(n, 10) : 1);
                                }}
                                sx={{ width: 100 }}
                                inputProps={{ min: 1, max: 10 }}
                            />
                        </Stack>

                        <TextField
                            multiline
                            minRows={4}
                            fullWidth
                            placeholder={t('playground.promptPlaceholder', {
                                defaultValue: 'Describe the image you want to generate…',
                            })}
                            value={prompt}
                            onChange={(e) => setPrompt(e.target.value)}
                            disabled={noModels}
                        />

                        <Box>
                            <Button
                                variant="contained"
                                onClick={handleGenerate}
                                disabled={sending || noModels || !prompt.trim() || !model}
                                startIcon={sending ? <CircularProgress size={16} /> : undefined}
                            >
                                {sending
                                    ? t('playground.generating', { defaultValue: 'Generating…' })
                                    : t('playground.generate', { defaultValue: 'Generate' })}
                            </Button>
                        </Box>

                        {results.length > 0 && (
                            <Box
                                sx={{
                                    display: 'grid',
                                    gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
                                    gap: 2,
                                }}
                            >
                                {results.map((img, idx) => {
                                    const src = img.url
                                        ? img.url
                                        : img.b64_json
                                            ? `data:image/png;base64,${img.b64_json}`
                                            : '';
                                    return (
                                        <Card key={idx} variant="outlined">
                                            <CardContent sx={{ p: 1, '&:last-child': { pb: 1 } }}>
                                                {src ? (
                                                    <Box
                                                        component="img"
                                                        src={src}
                                                        alt={`result-${idx}`}
                                                        sx={{ width: '100%', display: 'block', borderRadius: 1 }}
                                                    />
                                                ) : (
                                                    <Typography variant="caption" color="text.secondary">
                                                        empty
                                                    </Typography>
                                                )}
                                            </CardContent>
                                        </Card>
                                    );
                                })}
                            </Box>
                        )}
                    </Stack>
                </UnifiedCard>
            </CardGrid>
        </PageLayout>
    );
};

export default PlaygroundPage;
