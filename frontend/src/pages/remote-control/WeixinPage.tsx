import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const WeixinPage = () => {
    const config = usePlatformGuide('weixin');

    return (
        <PlatformBotPage
            platformId="weixin"
            platformName={config?.name || 'Weixin (微信)'}
            platformGuide={config?.guide}
        />
    );
};

export default WeixinPage;
