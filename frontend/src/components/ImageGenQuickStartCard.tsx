import { useState } from 'react';
import { Box, Tab, Tabs } from '@mui/material';
import UnifiedCard from '@/components/UnifiedCard';
import CodeBlock from '@/components/CodeBlock';

interface ImageGenQuickStartCardProps {
    baseUrl: string;
    /** Default model name shown in snippets. */
    model?: string;
    onCopy?: (text: string, label: string) => void;
}

type Lang = 'python' | 'typescript' | 'curl';

const TABS: { value: Lang; label: string; filename: string }[] = [
    { value: 'python', label: 'Python', filename: 'imagegen.py' },
    { value: 'typescript', label: 'TypeScript', filename: 'imagegen.ts' },
    { value: 'curl', label: 'curl', filename: 'imagegen.sh' },
];

const buildSnippet = (lang: Lang, baseUrl: string, model: string): string => {
    const endpoint = `${baseUrl}/tingly/imagegen/v1`;
    const prompt = 'A cozy cabin in a snowy forest at dusk, cinematic lighting';
    switch (lang) {
        case 'python':
            return `# pip install openai
from openai import OpenAI

client = OpenAI(
    base_url="${endpoint}",
    api_key="<TINGLY_MODEL_TOKEN>",  # GET /api/v1/token
)

resp = client.images.generate(
    model="${model}",
    prompt="${prompt}",
    size="1024x1024",
    quality="auto",
    n=1,
)

print(resp.data[0].url or resp.data[0].b64_json[:64])
`;
        case 'typescript':
            return `// npm i openai
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${endpoint}",
  apiKey: "<TINGLY_MODEL_TOKEN>", // GET /api/v1/token
});

const resp = await client.images.generate({
  model: "${model}",
  prompt: "${prompt}",
  size: "1024x1024",
  quality: "auto",
  n: 1,
});

console.log(resp.data[0].url ?? resp.data[0].b64_json?.slice(0, 64));
`;
        case 'curl':
            return `curl ${endpoint}/images/generations \\
  -H "Authorization: Bearer <TINGLY_MODEL_TOKEN>" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "prompt": "${prompt}",
    "size": "1024x1024",
    "quality": "auto",
    "n": 1
  }'
`;
    }
};

const ImageGenQuickStartCard: React.FC<ImageGenQuickStartCardProps> = ({
    baseUrl,
    model = 'gpt-image-1',
    onCopy,
}) => {
    const [tab, setTab] = useState<Lang>('python');
    const active = TABS.find((t) => t.value === tab)!;
    const code = buildSnippet(tab, baseUrl, model);

    return (
        <UnifiedCard
            size="full"
            title="Quick Start"
            subtitle="Call the image generation endpoint via the OpenAI SDK or curl. Token comes from GET /api/v1/token."
        >
            <Box>
                <Tabs
                    value={tab}
                    onChange={(_, v) => setTab(v)}
                    sx={{ minHeight: 32, mb: 1, '& .MuiTabs-indicator': { height: 3 } }}
                >
                    {TABS.map((t) => (
                        <Tab
                            key={t.value}
                            value={t.value}
                            label={t.label}
                            sx={{ minHeight: 32, py: 0.5, fontSize: '0.875rem' }}
                        />
                    ))}
                </Tabs>
                <CodeBlock
                    code={code}
                    language={tab === 'curl' ? 'bash' : tab}
                    filename={active.filename}
                    onCopy={onCopy ? (c) => onCopy(c, active.filename) : undefined}
                    maxHeight={360}
                    wrap={false}
                />
            </Box>
        </UnifiedCard>
    );
};

export default ImageGenQuickStartCard;
