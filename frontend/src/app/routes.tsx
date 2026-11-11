import { lazy } from "react";
import { LayoutDashboard, List, BarChart3, User, Settings, FileText } from "lucide-react";
const Detail = lazy(() => import("@/pages/Detail"));
const Dashboard = lazy(() => import("@/pages/Dashboard"));
const Records = lazy(() => import("@/pages/Records"));
const Reports = lazy(() => import("@/pages/Reports"));
const Profile = lazy(() => import("@/pages/Profile"));
const SettingsPage = lazy(() => import("@/pages/Settings"));
const Example = lazy(() => import("@/pages/Example"));
export const routes = [
  { path: "/", label: "Overview", icon: LayoutDashboard, component: Dashboard, navigation: true },
  { path: "/records", label: "Records", icon: List, component: Records, navigation: true },
  { path: "/reports", label: "Reports", icon: BarChart3, component: Reports, navigation: true },
  { path: "/profile", label: "Profile", icon: User, component: Profile, navigation: true },
  {
    path: "/settings",
    label: "Settings",
    icon: Settings,
    component: SettingsPage,
    navigation: true,
  },
  { path: "/example", label: "Example page", icon: FileText, component: Example, navigation: true },
  {
    path: "/records/:id",
    label: "Record details",
    icon: FileText,
    component: Detail,
    navigation: false,
  },
];
