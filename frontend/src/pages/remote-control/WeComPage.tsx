import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const WeComPage = () => {
    const config = usePlatformGuide('wecom');

    return (
        <PlatformBotPage
            platformId="wecom"
            platformName={config?.name || 'WeCom (企业微信)'}
            platformGuide={config?.guide}
        />
    );
};

export default WeComPage;
