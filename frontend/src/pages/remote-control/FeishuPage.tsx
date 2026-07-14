import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const FeishuPage = () => {
    const config = usePlatformGuide('feishu');

    return (
        <PlatformBotPage
            platformId="feishu"
            platformName={config?.name || 'Feishu (飞书)'}
            platformGuide={config?.guide}
        />
    );
};

export default FeishuPage;
