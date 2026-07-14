import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const LarkPage = () => {
    const config = usePlatformGuide('lark');

    return (
        <PlatformBotPage
            platformId="lark"
            platformName={config?.name || 'Lark'}
            platformGuide={config?.guide}
        />
    );
};

export default LarkPage;
