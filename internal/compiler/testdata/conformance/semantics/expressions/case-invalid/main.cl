class Main {
  main() : Object {
    case 0 of
      self : Int => 0;
      x : SELF_TYPE => 0;
      y : Missing => 0;
      z : Int => 0;
    esac
  };
};
